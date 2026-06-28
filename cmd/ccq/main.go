// ccq — clangd-powered C/C++ code-intelligence CLI for AI agents.
// Token-efficient navigation (callers/callees/explore/refs/symbols/macro) and
// symbol-level editing (rename), with a function-pointer dispatch heuristic that
// resolves what clangd alone won't. Zero Go dependencies; single static binary.
//
// A warm-clangd daemon keeps queries fast: the first query spawns it, later ones
// reuse it. Use --no-daemon to run clangd inline.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/swchen44/ccq/internal/cmd"
	"github.com/swchen44/ccq/internal/compdb"
	"github.com/swchen44/ccq/internal/config"
	"github.com/swchen44/ccq/internal/daemon"
	"github.com/swchen44/ccq/internal/lsp"
	"github.com/swchen44/ccq/internal/mcp"
)

const usage = `ccq — clangd-powered C/C++ code intelligence for agents

USAGE: ccq <command> [args] [flags]

NAVIGATE:
  search <query>          find symbols by name
  def <symbol>            show a symbol's definition source
  refs <symbol>           find all references
  callers <symbol>        who calls this (clangd + fnptr heuristic)
  callees <symbol>        what this calls
  impact <symbol> [-d N]  transitive callers (blast radius), default depth 3
  explore <symbol>        ONE-SHOT: source + callers + callees + blast radius
  symbols <file>          file outline
  macro <symbol>          macro expansion / signature (hover)

EDIT (symbol-level, Serena-parity; dry-run unless --apply):
  rename <symbol> <new> [--apply]            safe workspace-wide rename
  replace-body <symbol> <file> [--apply]     replace a symbol's whole definition
  insert-before <symbol> <file> [--apply]    insert content before a symbol
  insert-after <symbol> <file> [--apply]     insert content after a symbol

EXPORT (query with your own tools):
  export [--format json|sql|html] [--focus <sym> [-d N]] [--out f]
                          dump the call graph (whole repo, or a --focus neighborhood);
                          --format html writes a self-contained interactive graph
  fnptr                   validate the fn-pointer override table (ccq.fnptr.json)

SERVE:
  mcp                     serve ccq over the Model Context Protocol (stdio); tools: explore/callers/...

PROJECT:
  init                    detect/generate compile_commands.json (or compile_flags.txt, no-build)
  config                  show effective settings (ccq.json: allow/deny index filter)
  wait-index [--background] [--rebuild]   block until the index is ready (agents: run this first);
                          --background returns at once (poll 'ccq status'); --rebuild forces a fresh index
  status                  daemon running? + index mode/file count
  shutdown                stop the warm daemon
  version

FLAGS:
  -p <dir>      project root (default: cwd)
  --json        machine-readable output
  --clangd <p>  clangd binary (default: clangd on PATH)
  -d <n>        depth for impact
  --no-daemon   run clangd inline (no warm daemon)
  --incremental warm restart opens only git-changed files (needs a persisted clangd index)
  --compdb a.json[,b.json…]  use these compile_commands.json file(s) (any name; merged);
                each distinct set gets its own warm clangd (one per build config)
  --config <p>  settings file (default: ./ccq.json or ~/.config/ccq/ccq.json) — allow/deny index filter
`

const version = "ccq 0.5.0"

var queryCmds = map[string]bool{
	"search": true, "def": true, "show": true, "refs": true, "usages": true,
	"callers": true, "callees": true, "impact": true, "explore": true,
	"symbols": true, "macro": true, "rename": true, "export": true, "fnptr": true,
	"replace-body": true, "insert-before": true, "insert-after": true,
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}
	sub := os.Args[1]
	args, root, jsonOut, clangdBin, depth, noDaemon := parseFlags(os.Args[2:])
	format, outPath := flagVal("--format"), flagVal("--out")
	root = absOr(root)
	clangdBin = resolveClangd(clangdBin)
	// --incremental: open only changed files on a warm daemon (set via env so the
	// spawned daemon process inherits it). Requires a persisted clangd index.
	if hasFlag("--incremental") {
		os.Setenv("CCQ_INCREMENTAL", "1")
	}
	// --compdb: explicit compile_commands.json file(s) (any name; comma-separated,
	// auto-merged). Scope the daemon to this compile-DB set so distinct configs get
	// distinct warm clangds instead of colliding on the root key.
	cdbs := compdbPaths()
	compdbArg := strings.Join(cdbs, ",")
	// --config: load project/user settings (allow/deny index filter) and scope the
	// daemon to them so a different filter gets a different warm clangd.
	configArg := flagVal("--config")
	config.Load(root, configArg)
	daemon.SetKey(compdbArg + "\x00" + config.Key())
	exe, _ := os.Executable()

	switch sub {
	case "version", "-v", "--version":
		fmt.Println(version)
		return
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	case "init":
		dir, how, err := compdb.Ensure(root)
		if err != nil {
			fmt.Println("ERROR:", err)
			os.Exit(1)
		}
		fmt.Printf("compile_commands.json: %s (%s)\nclangd: %s\nready. try: ccq explore <symbol>\n", dir, how, clangdBin)
		return
	case "status":
		if st, err := daemon.Status(root); err == nil {
			fmt.Printf("daemon: running — index ready (%s, %d files)\n", st.Mode, st.Files)
		} else if daemon.IsIndexing(root) {
			fmt.Println("daemon: indexing… (not ready yet; re-run `ccq status` or `ccq wait-index`)")
		} else {
			fmt.Println("daemon: not running")
		}
		return
	case "wait-index": // block until the index is ready (so an agent can query safely)
		if hasFlag("--rebuild") {
			daemon.Shutdown(root)
			cacheDir := filepath.Join(root, ".cache", "clangd")
			if _, e := os.Stat(cacheDir); e == nil {
				fmt.Fprintf(os.Stderr, "warning: removing %s — this is clangd's index cache, shared with VS Code / your editor's clangd (they will re-index too).\n", cacheDir)
				os.RemoveAll(cacheDir)
			}
		}
		if hasFlag("--background") {
			if err := daemon.Spawn(root, exe, clangdBin, compdbArg, configArg); err != nil {
				fmt.Println("ERROR:", err)
				os.Exit(1)
			}
			fmt.Println("indexing started in background — poll `ccq status` until ready.")
			return
		}
		st, err := daemon.EnsureReady(root, exe, clangdBin, compdbArg, configArg)
		if err != nil {
			fmt.Println("ERROR:", err)
			os.Exit(1)
		}
		note := ""
		if st.Mode == "no-build" {
			note = " (no-build: dynamic index, best-effort completion)"
		}
		fmt.Printf("index ready: %s, %d files%s\n", st.Mode, st.Files, note)
		return
	case "shutdown":
		daemon.Shutdown(root)
		fmt.Println("daemon: stopped")
		return
	case "config": // show the effective settings (source, allow/deny, problems)
		if config.Source() == "" {
			fmt.Println("no ccq.json found (looked at: ./ccq.json, ~/.config/ccq/ccq.json, --config)")
			fmt.Println("all files indexed (no allow/deny filter).")
		} else {
			fmt.Printf("config: %s\n", config.Source())
			s := config.Get()
			fmt.Printf("  allow: %v\n  deny:  %v\n  fallbackFlags: %v\n", s.Allow, s.Deny, s.FallbackFlags)
		}
		for _, w := range config.Warnings() {
			fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
		}
		return
	case "mcp": // serve ccq over the Model Context Protocol (JSON-RPC/stdio)
		runner := func(sub, arg, croot string) (string, error) {
			if croot == "" {
				croot = root
			}
			croot = absOr(croot)
			cd := resolveClangd(clangdBin)
			req := cmd.Request{Cmd: sub, Args: []string{arg}, Depth: 3}
			return daemon.Query(croot, exe, cd, compdbArg, configArg, req)
		}
		mcp.Serve(os.Stdin, os.Stdout, runner, root)
		return
	case "__daemon": // internal: the warm server process
		ccDir := resolveCompileDB(root, cdbs)
		maxWait, baseline := cmd.IndexWaits(isBig(root))
		if err := daemon.Serve(root, clangdBin, ccDir, 1200, maxWait, baseline); err != nil {
			os.Exit(1)
		}
		return
	}

	if !queryCmds[sub] {
		fmt.Printf("unknown command: %s\n\n%s", sub, usage)
		os.Exit(1)
	}

	if format == "" {
		format = "json"
	}
	warnCompileDB(root) // surface degraded/no-build mode in BOTH daemon and inline paths
	req := cmd.Request{Cmd: sub, Args: normalize(sub, args), JSON: jsonOut, Depth: depth,
		Apply: hasFlag("--apply"), Format: format, OutPath: outPath, Focus: flagVal("--focus")}

	// Daemon path (default): fast warm clangd.
	if !noDaemon {
		out, err := daemon.Query(root, exe, clangdBin, compdbArg, configArg, req)
		if err == nil {
			fmt.Print(out)
			return
		}
		fmt.Fprintln(os.Stderr, "warning: daemon unavailable ("+err.Error()+"), running inline")
	}

	// Inline path.
	runInline(root, clangdBin, req)
}

// warnCompileDB tells the user when ccq is running without a real build database —
// shown for every query (daemon or inline), since the warm daemon path used to hide it.
func warnCompileDB(root string) {
	if len(compdbPaths()) > 0 {
		return // explicit --compdb provides a real compile database
	}
	if compdb.Locate(root) == "" {
		fmt.Fprintln(os.Stderr, "warning: no compile_commands.json/compile_flags.txt — degraded (same-file) mode. Run `ccq init`.")
	} else if compdb.IsNoBuild(root) {
		fmt.Fprintln(os.Stderr, "note: no-build mode (compile_flags.txt) — cross-file works but accuracy is lower than a real build (#ifdef over-included, no -D).")
	}
}

// compdbPaths returns the absolute paths given via --compdb (comma-separated), or nil.
func compdbPaths() []string {
	v := flagVal("--compdb")
	if v == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, absOr(p))
		}
	}
	return out
}

// resolveCompileDB returns clangd's --compile-commands-dir: a staged merge of the
// --compdb files if given, else the auto-located compile_commands.json/compile_flags.txt.
func resolveCompileDB(root string, cdbs []string) string {
	if len(cdbs) > 0 {
		dir, err := compdb.Stage(cdbs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			os.Exit(1)
		}
		return dir
	}
	return compdb.Locate(root)
}

func runInline(root, clangdBin string, req cmd.Request) {
	ccDir := resolveCompileDB(root, compdbPaths())
	client, err := lsp.Start(clangdBin, root, ccDir)
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
	defer client.Close()
	client.OpenAll(root, 1200)
	maxWait, baseline := cmd.IndexWaits(isBig(root))
	client.WaitIndex(maxWait, baseline)
	c := &cmd.Ctx{Client: client, Root: root, Out: os.Stdout}
	if !c.Dispatch(req) {
		fmt.Printf("unknown command: %s\n", req.Cmd)
		os.Exit(1)
	}
}

// normalize resolves file-path args (symbols command takes a file).
func normalize(sub string, args []string) []string {
	if sub == "symbols" && len(args) > 0 {
		args[0] = absOr(args[0])
	}
	return args
}

func parseFlags(in []string) (args []string, root string, jsonOut bool, clangdBin string, depth int, noDaemon bool) {
	depth = 3
	for i := 0; i < len(in); i++ {
		switch in[i] {
		case "-p", "--path":
			if i+1 < len(in) {
				root = in[i+1]
				i++
			}
		case "--json":
			jsonOut = true
		case "--clangd":
			if i+1 < len(in) {
				clangdBin = in[i+1]
				i++
			}
		case "-d", "--depth":
			if i+1 < len(in) {
				depth, _ = strconv.Atoi(in[i+1])
				i++
			}
		case "--no-daemon":
			noDaemon = true
		case "--apply", "--incremental", "--rebuild", "--background":
		case "--format", "--out", "--focus", "--compdb", "--config":
			i++ // value consumed via flagVal
		default:
			args = append(args, in[i])
		}
	}
	return
}

func hasFlag(f string) bool {
	for _, a := range os.Args {
		if a == f {
			return true
		}
	}
	return false
}

// flagVal returns the value following flag f in os.Args, or "".
func flagVal(f string) string {
	for i, a := range os.Args {
		if a == f && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func absOr(p string) string {
	if p == "" {
		wd, _ := os.Getwd()
		return wd
	}
	abs, _ := filepath.Abs(p)
	return abs
}

func resolveClangd(bin string) string {
	if bin != "" {
		return bin
	}
	// Prefer a clangd bundled next to the ccq binary (release --bundle-clangd).
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, name := range []string{"clangd", "clangd.exe"} {
			p := filepath.Join(dir, name)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p
			}
		}
	}
	if p, err := exec.LookPath("clangd"); err == nil {
		return p
	}
	return "clangd"
}

func isBig(root string) bool {
	n := 0
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			switch filepath.Ext(p) {
			case ".c", ".cc", ".cpp", ".cxx":
				n++
			}
		}
		if n > 80 {
			return filepath.SkipAll
		}
		return nil
	})
	return n > 80
}
