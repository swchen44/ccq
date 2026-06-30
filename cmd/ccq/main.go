// ccq — clangd-powered C/C++ code-intelligence CLI for AI agents.
// Token-efficient navigation (callers/callees/explore/refs/symbols/macro) and
// symbol-level editing (rename), with a function-pointer dispatch heuristic that
// resolves what clangd alone won't. Zero Go dependencies; single static binary.
//
// A warm-clangd daemon keeps queries fast: the first query spawns it, later ones
// reuse it. Use --no-daemon to run clangd inline.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/swchen44/ccq/internal/cache"
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
  cache [list|clean|path]   inspect/clean index caches (clean: --all|--project p|--older-than N [--index])
  doctor                  dump environment (versions, config, compile DB, caches) for debugging
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

const version = "ccq 0.6.4"

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
			fmt.Printf("  allow: %v\n  deny:  %v\n", s.Allow, s.Deny)
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
	case "cache": // inspect / clean ccq + clangd index caches
		cacheCmd(args)
		return
	case "doctor": // dump environment for debugging
		doctorCmd(root, clangdBin, cdbs)
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
	var ccDir string
	if len(cdbs) > 0 {
		dir, err := compdb.Stage(cdbs)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			os.Exit(1)
		}
		ccDir = dir
	} else {
		ccDir = compdb.Locate(root)
	}
	// a ccq.json allow/deny filter must also drop denied files from the compile DB,
	// or clangd's background index would index them anyway.
	return compdb.ApplyFilter(ccDir)
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

// doctorCmd dumps the environment to help debug setup problems.
func doctorCmd(root, clangdBin string, cdbs []string) {
	var hints []string
	ok := func(b bool) string {
		if b {
			return "✓"
		}
		return "✗"
	}
	fmt.Printf("ccq doctor\n\n")
	fmt.Printf("  ccq version : %s\n", version)
	fmt.Printf("  OS / arch   : %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("  project (-p): %s\n", root)

	// clangd
	cpath, cver := clangdInfo(clangdBin)
	fmt.Printf("  clangd      : %s %s\n", ok(cver != ""), cpath)
	if cver != "" {
		fmt.Printf("                %s\n", cver)
	} else {
		hints = append(hints, "clangd not found — install it or pass --clangd /path/to/clangd")
	}

	// config
	fmt.Printf("\n  config      : ")
	if config.Source() == "" {
		fmt.Println("(none) — all files indexed (no allow/deny filter)")
	} else {
		s := config.Get()
		fmt.Printf("%s\n                allow=%v deny=%v\n", config.Source(), s.Allow, s.Deny)
	}
	for _, w := range config.Warnings() {
		fmt.Printf("                ✗ %s\n", w)
		hints = append(hints, "fix the config problem above (ccq.json)")
	}

	// compile database
	ccDir := resolveCompileDB(root, cdbs)
	mode, entries := compileDBInfo(root, ccDir, cdbs)
	fmt.Printf("\n  compile DB  : %s %s", ok(mode != "none"), mode)
	if entries >= 0 {
		fmt.Printf(" (%d entries)", entries)
	}
	if len(cdbs) > 0 {
		fmt.Printf(" via --compdb %v", cdbs)
	}
	fmt.Println()
	switch mode {
	case "none":
		hints = append(hints, "no compile database — run `ccq init`, or pass --compdb; navigation works but accuracy is degraded")
	case "no-build":
		hints = append(hints, "no-build mode (compile_flags.txt) — accuracy is lower than a real build (#ifdef over-included, no -D)")
	}

	// caches (this project + totals)
	var ccqTotal, clangdHere int64
	for _, e := range cache.List() {
		ccqTotal += e.Size
		if e.Kind == "clangd-index" && e.Project == root {
			clangdHere = e.Size
		}
	}
	fmt.Printf("\n  cache (ccq) : %s total at %s\n", humanSize(ccqTotal), cache.Base())
	fmt.Printf("  clangd index: %s at %s/.cache/clangd  (shared with VS Code / editor clangd)\n", humanSize(clangdHere), root)

	// daemon
	if st, err := daemon.Status(root); err == nil {
		fmt.Printf("  daemon      : running — index ready (%s, %d files)\n", st.Mode, st.Files)
	} else if daemon.IsIndexing(root) {
		fmt.Printf("  daemon      : indexing…\n")
	} else {
		fmt.Printf("  daemon      : not running\n")
	}

	if len(hints) > 0 {
		fmt.Println("\nhints:")
		for _, h := range hints {
			fmt.Printf("  • %s\n", h)
		}
	}
}

func clangdInfo(clangdBin string) (path, version string) {
	path = resolveClangd(clangdBin)
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return path, ""
	}
	return path, strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
}

// compileDBInfo returns the index mode and entry count (-1 if not countable).
func compileDBInfo(root, ccDir string, cdbs []string) (mode string, entries int) {
	entries = -1
	if ccDir != "" {
		if b, err := os.ReadFile(filepath.Join(ccDir, "compile_commands.json")); err == nil {
			mode = "compile_commands"
			var arr []json.RawMessage
			if json.Unmarshal(b, &arr) == nil {
				entries = len(arr)
			}
			return
		}
	}
	if compdb.IsNoBuild(root) {
		return "no-build", -1
	}
	return "none", -1
}

// cacheCmd implements `ccq cache [list|clean|path]`.
func cacheCmd(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "path":
		fmt.Println(cache.Base())
	case "clean":
		opts := cache.CleanOpts{All: hasFlag("--all"), Index: hasFlag("--index")}
		if p := flagVal("--project"); p != "" {
			opts.Project = absOr(p)
		}
		if v := flagVal("--older-than"); v != "" {
			if d, _ := strconv.Atoi(v); d > 0 {
				opts.OlderThan = time.Duration(d) * 24 * time.Hour
			}
		}
		if !opts.All && opts.Project == "" && opts.OlderThan == 0 {
			fmt.Println("nothing selected. use --all | --project <path> | --older-than <days> (add --index to also clear clangd's index).")
			return
		}
		if opts.Index {
			fmt.Fprintln(os.Stderr, "warning: --index also removes <root>/.cache/clangd — shared with VS Code / your editor's clangd (they will re-index).")
		}
		removed := cache.Clean(opts)
		var total int64
		for _, e := range removed {
			total += e.Size
			fmt.Printf("removed %-13s %s (%s)\n", e.Kind, dispProject(e), humanSize(e.Size))
		}
		fmt.Printf("freed %s across %d item(s).\n", humanSize(total), len(removed))
	default: // list
		entries := cache.List()
		if len(entries) == 0 {
			fmt.Println("no caches yet. (run a query to warm a project.)")
			return
		}
		var total int64
		fmt.Printf("%-13s %-15s %9s  %-16s  %s\n", "KIND", "MODE", "SIZE", "MODIFIED", "PROJECT")
		for _, e := range entries {
			total += e.Size
			run := ""
			if e.Running {
				run = " [running]"
			}
			fmt.Printf("%-13s %-15s %9s  %-16s  %s%s\n", e.Kind, e.Mode, humanSize(e.Size),
				e.Modified.Format("2006-01-02 15:04"), dispProject(e), run)
		}
		fmt.Printf("\ntotal: %s. note: clangd-index is shared with editors (VS Code).\n", humanSize(total))
		fmt.Println("clean: `ccq cache clean --older-than 14` | `--project <p>` | `--all` (add --index for clangd's index).")
	}
}

func dispProject(e cache.Entry) string {
	if e.Project != "" {
		return e.Project
	}
	return e.Dir
}

func humanSize(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0fK", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
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
		case "--apply", "--incremental", "--rebuild", "--background", "--all", "--index":
		case "--format", "--out", "--focus", "--compdb", "--config", "--project", "--older-than":
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
