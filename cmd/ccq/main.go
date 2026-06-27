// ccq — clangd-powered C/C++ code-intelligence CLI for AI agents.
// Token-efficient navigation (callers/callees/explore/refs/symbols/macro) and
// symbol-level editing (rename), with a function-pointer dispatch heuristic that
// resolves what clangd alone won't. Zero Go dependencies; single static binary.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/swchen44/ccq/internal/cmd"
	"github.com/swchen44/ccq/internal/compdb"
	"github.com/swchen44/ccq/internal/lsp"
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

EDIT (symbol-level, Serena-parity):
  rename <symbol> <new> [--apply]   safe workspace-wide rename (dry-run by default)

PROJECT:
  init                    detect/generate compile_commands.json + warm clangd
  version

FLAGS:
  -p <dir>      project root (default: cwd)
  --json        machine-readable output
  --clangd <p>  clangd binary (default: clangd on PATH)
  -d <n>        depth for impact
`

const version = "ccq 0.1.0"

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}
	sub := os.Args[1]
	args, root, jsonOut, clangdBin, depth := parseFlags(os.Args[2:])

	switch sub {
	case "version", "-v", "--version":
		fmt.Println(version)
		return
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	}

	root = absOr(root)
	clangdBin = resolveClangd(clangdBin)

	if sub == "init" {
		dir, how, err := compdb.Ensure(root)
		if err != nil {
			fmt.Println("ERROR:", err)
			os.Exit(1)
		}
		fmt.Printf("compile_commands.json: %s (%s)\n", dir, how)
		fmt.Println("clangd:", clangdBin)
		fmt.Println("ready. try: ccq explore <symbol>")
		return
	}

	// locate compile_commands (warn but continue in degraded mode if missing)
	ccDir := compdb.Locate(root)
	if ccDir == "" {
		fmt.Fprintln(os.Stderr, "warning: no compile_commands.json found — clangd runs in degraded (same-file) mode. Run `ccq init` for full accuracy.")
	}

	client, err := lsp.Start(clangdBin, root, ccDir)
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
	defer client.Close()
	// Prime clangd's index by opening project source files (workspace/symbol and
	// cross-file call hierarchy need this; the background static index alone is
	// not reliably populated on a cold project).
	client.OpenAll(root, 1200)
	maxWait, baseline := cmd.IndexWaits(isBig(root))
	client.WaitIndex(maxWait, baseline)

	c := &cmd.Ctx{Client: client, Root: root, JSON: jsonOut}

	need := func(n int) {
		if len(args) < n {
			fmt.Printf("usage: ccq %s ...\n", sub)
			os.Exit(1)
		}
	}

	switch sub {
	case "search":
		need(1)
		c.Search(args[0])
	case "def", "show":
		need(1)
		c.Def(args[0])
	case "refs", "usages":
		need(1)
		c.Refs(args[0])
	case "callers":
		need(1)
		c.Callers(args[0])
	case "callees":
		need(1)
		c.Callees(args[0])
	case "impact":
		need(1)
		c.Impact(args[0], depth)
	case "explore":
		need(1)
		c.Explore(args[0])
	case "symbols":
		need(1)
		c.Symbols(absOr(args[0]))
	case "macro":
		need(1)
		c.Macro(args[0])
	case "rename":
		need(2)
		c.Rename(args[0], args[1], hasFlag("--apply"))
	default:
		fmt.Printf("unknown command: %s\n\n%s", sub, usage)
		os.Exit(1)
	}
}

func parseFlags(in []string) (args []string, root string, jsonOut bool, clangdBin string, depth int) {
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
		case "--apply":
			// consumed by hasFlag
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
	if p, err := exec.LookPath("clangd"); err == nil {
		return p
	}
	return "clangd"
}

// isBig is a rough heuristic to give clangd more index time on large projects.
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
