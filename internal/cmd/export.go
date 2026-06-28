package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/swchen44/ccq/internal/fnptr"
	"github.com/swchen44/ccq/internal/lsp"
)

type exNode struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}
type exEdge struct {
	Src  string `json:"src"`
	Dst  string `json:"dst"`
	Kind string `json:"kind"` // "calls" | "fnptr"
}

// Export dumps the project's symbols + call graph as JSON, a SQLite-loadable SQL
// script, or a self-contained interactive HTML knowledge graph. With --focus it
// builds just a neighborhood (BFS to --depth) — fast on large repos; without it,
// the whole repo (slow on big trees, fine for json/sql piping).
//
//	ccq export --format sql  | sqlite3 g.db          # query with plain SQL
//	ccq export --format html --focus lookupCommand --out g.html
func (c *Ctx) Export(format, outPath, focus string, depth int) {
	var nodes []exNode
	var edges []exEdge
	if focus != "" {
		if depth <= 0 {
			depth = 2
		}
		nodes, edges = c.buildNeighborhood(focus, depth)
	} else {
		nodes, edges = c.buildFullGraph()
	}

	var out io.Writer = c.Out
	if outPath != "" {
		if f, err := os.Create(outPath); err == nil {
			defer f.Close()
			out = f
		}
	}
	switch format {
	case "sql":
		writeSQL(out, nodes, edges)
	case "html":
		writeHTML(out, nodes, edges, focus)
	default:
		b, _ := json.MarshalIndent(map[string]any{"focus": focus, "nodes": nodes, "edges": edges}, "", " ")
		fmt.Fprintln(out, string(b))
	}
	if outPath != "" {
		fmt.Fprintf(c.Out, "exported %d nodes, %d edges -> %s\n", len(nodes), len(edges), outPath)
	}
}

// buildNeighborhood does a BFS around focus (callers + callees) to the given
// depth, reusing the same proven callerNames/calleeNames paths as the CLI.
func (c *Ctx) buildNeighborhood(focus string, depth int) ([]exNode, []exEdge) {
	nodes := map[string]exNode{focus: {Name: focus, Kind: "function"}}
	var edges []exEdge
	eseen := map[string]bool{}
	addNode := func(n string) {
		if _, ok := nodes[n]; !ok {
			nodes[n] = exNode{Name: n, Kind: "function"}
		}
	}
	addEdge := func(s, d, k string) {
		key := s + ">" + d + "|" + k
		if !eseen[key] {
			eseen[key] = true
			edges = append(edges, exEdge{s, d, k})
		}
	}
	visited := map[string]bool{focus: true}
	frontier := []string{focus}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []string
		for _, fn := range frontier {
			real, heur := c.callerNames(fn)
			for _, x := range real {
				addNode(x)
				addEdge(x, fn, "calls")
				if !visited[x] {
					visited[x] = true
					next = append(next, x)
				}
			}
			for _, h := range heur {
				addNode(h.Func)
				addEdge(h.Func, fn, "fnptr")
				if !visited[h.Func] {
					visited[h.Func] = true
					next = append(next, h.Func)
				}
			}
			if file, pos, ok := c.resolveSymbol(fn); ok {
				for _, y := range c.calleeNames(fn, file, pos) {
					addNode(y)
					addEdge(fn, y, "calls")
					if !visited[y] {
						visited[y] = true
						next = append(next, y)
					}
				}
			}
		}
		frontier = next
	}
	out := make([]exNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n)
	}
	return out, edges
}

// buildFullGraph dumps every symbol + the whole call graph (slow on large repos).
func (c *Ctx) buildFullGraph() ([]exNode, []exEdge) {
	var nodes []exNode
	var edges []exEdge
	seenNode := map[string]bool{}
	seenEdge := map[string]bool{}
	funcFiles := map[string]string{} // function name -> file (for call hierarchy)
	funcPos := map[string]lsp.Position{}

	// 1) nodes via documentSymbol over every source file
	for _, f := range sourceFiles(c.Root) {
		res, err := c.Client.DocumentSymbol(f)
		if err != nil || res == nil {
			continue
		}
		// clangd returns flat SymbolInformation[]: the range lives in location.range.
		var syms []struct {
			Name     string       `json:"name"`
			Kind     int          `json:"kind"`
			Location lsp.Location `json:"location"`
		}
		json.Unmarshal(res, &syms)
		for _, s := range syms {
			key := s.Name + "|" + f
			if seenNode[key] {
				continue
			}
			seenNode[key] = true
			line := s.Location.Range.Start.Line
			nodes = append(nodes, exNode{s.Name, kindName(s.Kind), f, line + 1})
			if isFuncKind(s.Kind) {
				funcFiles[s.Name] = f
				// position the cursor on the name for call hierarchy.
				pos := s.Location.Range.Start
				if col := nameColumn(f, line, s.Name); col >= 0 {
					pos.Character = col
				}
				funcPos[s.Name] = pos
			}
		}
	}

	// 2) call edges via incomingCalls for each function (clangd's outgoingCalls
	// is unreliable; incomingCalls is solid). For each function F, every caller
	// X yields an edge X->F — the same call graph, built from the caller side.
	// Use resolveSymbol for the cursor (same proven path as `ccq callers`).
	_ = funcFiles
	_ = funcPos
	for name := range funcFiles {
		file, pos, ok := c.resolveSymbol(name)
		if !ok {
			continue
		}
		items, _ := c.Client.PrepareCallHierarchy(file, pos)
		if len(items) == 0 {
			continue
		}
		callers, _ := c.Client.IncomingCalls(items[0])
		for _, x := range callers {
			ek := x.Name + ">" + name + "|calls"
			if !seenEdge[ek] {
				seenEdge[ek] = true
				edges = append(edges, exEdge{x.Name, name, "calls"})
			}
		}
	}

	// 3) fnptr heuristic edges (dispatcher -> handler), once per handler node
	for _, n := range nodes {
		if n.Kind != "function" {
			continue
		}
		for _, h := range fnptr.Callers(c.Root, n.Name) {
			ek := h.Func + ">" + n.Name + "|fnptr"
			if !seenEdge[ek] {
				seenEdge[ek] = true
				edges = append(edges, exEdge{h.Func, n.Name, "fnptr"})
			}
		}
	}

	// Default: write the dump to c.Out (stdout in direct mode, or the daemon's
	// response back to the client). --out writes to a file instead.
	var out io.Writer = c.Out
	var fh *os.File
	if outPath != "" {
		if f, err := os.Create(outPath); err == nil {
			fh = f
			defer fh.Close()
			out = fh
		}
	}

	if format == "sql" {
		writeSQL(out, nodes, edges)
	} else {
		b, _ := json.MarshalIndent(map[string]any{"nodes": nodes, "edges": edges}, "", " ")
		fmt.Fprintln(out, string(b))
	}
	if outPath != "" {
		fmt.Fprintf(c.Out, "exported %d nodes, %d edges -> %s\n", len(nodes), len(edges), outPath)
	}
}

func writeSQL(out io.Writer, nodes []exNode, edges []exEdge) {
	fmt.Fprintln(out, "BEGIN;")
	fmt.Fprintln(out, "CREATE TABLE IF NOT EXISTS nodes(name TEXT, kind TEXT, file TEXT, line INT);")
	fmt.Fprintln(out, "CREATE TABLE IF NOT EXISTS edges(src TEXT, dst TEXT, kind TEXT);")
	for _, n := range nodes {
		fmt.Fprintf(out, "INSERT INTO nodes VALUES('%s','%s','%s',%d);\n",
			sqlEsc(n.Name), n.Kind, sqlEsc(n.File), n.Line)
	}
	for _, e := range edges {
		fmt.Fprintf(out, "INSERT INTO edges VALUES('%s','%s','%s');\n",
			sqlEsc(e.Src), sqlEsc(e.Dst), e.Kind)
	}
	fmt.Fprintln(out, "COMMIT;")
}

func sqlEsc(s string) string { return strings.ReplaceAll(s, "'", "''") }

// writeHTML emits a self-contained, offline, zero-dependency interactive knowledge
// graph (vanilla-JS force-directed SVG; no CDN, no build). Node roles (focus /
// caller / callee) are derived in-page from focus + edges.
func writeHTML(out io.Writer, nodes []exNode, edges []exEdge, focus string) {
	payload, _ := json.Marshal(map[string]any{"focus": focus, "nodes": nodes, "edges": edges})
	title := "ccq call graph"
	if focus != "" {
		title = "ccq — " + focus
	}
	h := strings.ReplaceAll(graphHTMLTemplate, "__TITLE__", title)
	h = strings.ReplaceAll(h, "__PAYLOAD__", string(payload))
	fmt.Fprint(out, h)
}

const graphHTMLTemplate = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>__TITLE__</title>
<style>:root{--bg:#0d1117;--fg:#c9d1d9;--muted:#8b949e;--edge:#30363d;--fnptr:#d29922}
*{box-sizing:border-box}html,body{margin:0;height:100%;background:var(--bg);color:var(--fg);font:14px/1.4 -apple-system,Segoe UI,Roboto,sans-serif}
header{padding:10px 16px;border-bottom:1px solid var(--edge)}header h1{margin:0;font-size:16px}header p{margin:4px 0 0;color:var(--muted);font-size:12px}
.legend{display:flex;gap:14px;margin-top:8px;flex-wrap:wrap;font-size:12px}.legend span{display:inline-flex;align-items:center;gap:5px}
.dot{width:11px;height:11px;border-radius:50%;display:inline-block}svg{width:100vw;height:calc(100vh - 88px);cursor:grab;display:block}
.link{stroke:var(--edge);stroke-width:1.2}.link.fnptr{stroke:var(--fnptr);stroke-dasharray:4 3;stroke-width:1.6}
.node circle{stroke:#0d1117;stroke-width:1.5;cursor:pointer}.node text{fill:var(--fg);font-size:11px;pointer-events:none;paint-order:stroke;stroke:#0d1117;stroke-width:3px}
.node.dim{opacity:.15}.link.dim{opacity:.06}</style></head><body>
<header><h1>__TITLE__</h1><p>Generated by <code>ccq export --format html</code> — drag nodes, hover to isolate a neighborhood. Self-contained, offline.</p>
<div class="legend"><span><i class="dot" style="background:#e3b341"></i>focus</span><span><i class="dot" style="background:#58a6ff"></i>caller</span>
<span><i class="dot" style="background:#3fb950"></i>callee</span><span><i class="dot" style="background:#8b949e"></i>other</span>
<span><i class="dot" style="background:#d29922"></i>— — fn-pointer edge</span></div></header><svg id="g"></svg><script>
const DATA=__PAYLOAD__;const COLOR={focus:"#e3b341",caller:"#58a6ff",callee:"#3fb950",other:"#8b949e"};
const svg=document.getElementById("g");let W=svg.clientWidth,H=svg.clientHeight;const NS="http://www.w3.org/2000/svg";
svg.innerHTML='<defs><marker id="arr" viewBox="0 0 10 10" refX="18" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" fill="#484f58"/></marker></defs>';
const focus=DATA.focus;const callers=new Set(DATA.edges.filter(e=>e.dst===focus).map(e=>e.src));const callees=new Set(DATA.edges.filter(e=>e.src===focus).map(e=>e.dst));
DATA.nodes.forEach(n=>{n.role=n.id===focus?"focus":callers.has(n.id)?"caller":callees.has(n.id)?"callee":"other";});
const idx=new Map(DATA.nodes.map(n=>[n.id,n]));DATA.nodes.forEach(n=>{n.x=W/2+(Math.random()-.5)*300;n.y=H/2+(Math.random()-.5)*300;n.vx=0;n.vy=0;});
const links=DATA.edges.filter(e=>idx.has(e.src)&&idx.has(e.dst)).map(e=>({s:idx.get(e.src),t:idx.get(e.dst),kind:e.kind}));
const deg=new Map();DATA.nodes.forEach(n=>deg.set(n.id,0));links.forEach(l=>{deg.set(l.s.id,deg.get(l.s.id)+1);deg.set(l.t.id,deg.get(l.t.id)+1);});
const gL=document.createElementNS(NS,"g");svg.appendChild(gL);const gN=document.createElementNS(NS,"g");svg.appendChild(gN);
const lineEls=links.map(l=>{const e=document.createElementNS(NS,"line");e.setAttribute("class","link"+(l.kind==="fnptr"?" fnptr":""));e.setAttribute("marker-end","url(#arr)");gL.appendChild(e);return e;});
const nodeEls=DATA.nodes.map(n=>{const g=document.createElementNS(NS,"g");g.setAttribute("class","node");const r=(n.role==="focus")?13:(6+Math.min(8,deg.get(n.id)));
const c=document.createElementNS(NS,"circle");c.setAttribute("r",r);c.setAttribute("fill",COLOR[n.role]||COLOR.other);
const t=document.createElementNS(NS,"text");t.setAttribute("x",r+3);t.setAttribute("y",4);t.textContent=n.id;g.appendChild(c);g.appendChild(t);gN.appendChild(g);g._n=n;drag(g,n);hover(g,n);return g;});
function tick(){for(let i=0;i<DATA.nodes.length;i++)for(let j=i+1;j<DATA.nodes.length;j++){const a=DATA.nodes[i],b=DATA.nodes[j];let dx=a.x-b.x,dy=a.y-b.y,d2=dx*dx+dy*dy||1;let f=11000/d2,d=Math.sqrt(d2);dx/=d;dy/=d;a.vx+=dx*f;a.vy+=dy*f;b.vx-=dx*f;b.vy-=dy*f;}
links.forEach(l=>{let dx=l.t.x-l.s.x,dy=l.t.y-l.s.y,d=Math.sqrt(dx*dx+dy*dy)||1,f=(d-120)*0.015;dx/=d;dy/=d;l.s.vx+=dx*f;l.s.vy+=dy*f;l.t.vx-=dx*f;l.t.vy-=dy*f;});
DATA.nodes.forEach(n=>{n.vx+=(W/2-n.x)*0.0012;n.vy+=(H/2-n.y)*0.0012;if(n._fix){n.x=n._fx;n.y=n._fy;}else{n.x+=n.vx*=.85;n.y+=n.vy*=.85;}});
lineEls.forEach((e,i)=>{const l=links[i];e.setAttribute("x1",l.s.x);e.setAttribute("y1",l.s.y);e.setAttribute("x2",l.t.x);e.setAttribute("y2",l.t.y);});
nodeEls.forEach(g=>g.setAttribute("transform","translate("+g._n.x+","+g._n.y+")"));requestAnimationFrame(tick);}tick();
function pt(ev){const r=svg.getBoundingClientRect();return{x:ev.clientX-r.left,y:ev.clientY-r.top};}
function drag(g,n){g.addEventListener("mousedown",e=>{n._fix=true;const mv=ev=>{const p=pt(ev);n._fx=p.x;n._fy=p.y;};const up=()=>{n._fix=false;document.removeEventListener("mousemove",mv);document.removeEventListener("mouseup",up);};document.addEventListener("mousemove",mv);document.addEventListener("mouseup",up);e.preventDefault();});}
function hover(g,n){g.addEventListener("mouseenter",()=>{const keep=new Set([n.id]);links.forEach(l=>{if(l.s.id===n.id)keep.add(l.t.id);if(l.t.id===n.id)keep.add(l.s.id);});nodeEls.forEach(e=>e.classList.toggle("dim",!keep.has(e._n.id)));lineEls.forEach((e,i)=>e.classList.toggle("dim",links[i].s.id!==n.id&&links[i].t.id!==n.id));});g.addEventListener("mouseleave",()=>{nodeEls.forEach(e=>e.classList.remove("dim"));lineEls.forEach(e=>e.classList.remove("dim"));});}
window.addEventListener("resize",()=>{W=svg.clientWidth;H=svg.clientHeight;});</script></body></html>`

func sourceFiles(root string) []string {
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				b := filepath.Base(p)
				if b == ".git" || b == "build" || b == ".cache" || b == "node_modules" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".c", ".h", ".cc", ".cpp", ".cxx", ".hpp":
			out = append(out, p)
		}
		return nil
	})
	return out
}
