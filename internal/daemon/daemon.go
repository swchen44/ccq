// Package daemon keeps a clangd-backed ccq session warm so repeated queries are
// fast (no re-index per command). The first query spawns a background daemon;
// subsequent queries connect to it over an IPC socket.
//
// Cross-platform: a Unix domain socket on macOS/Linux, a localhost TCP port on
// Windows. The chosen address is written to a per-project state file.
package daemon

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/swchen44/ccq/internal/cmd"
	"github.com/swchen44/ccq/internal/gitdiff"
	"github.com/swchen44/ccq/internal/lsp"
)

const idleTimeout = 30 * time.Minute

// reqMsg/respMsg are the IPC envelope.
type reqMsg struct {
	cmd.Request
	Shutdown bool `json:"shutdown,omitempty"`
	Ping     bool `json:"ping,omitempty"`
}
type respMsg struct {
	Output string `json:"output"`
	Err    string `json:"err,omitempty"`
	Mode   string `json:"mode,omitempty"`  // compile_commands | no-build | none
	Files  int    `json:"files,omitempty"` // source files opened (index breadth)
}

// keySalt scopes the daemon's socket/state to a compile-DB context. With it,
// distinct --compdb sets get distinct warm daemons (a clangd per build config)
// instead of colliding on the bare root key. Set identically on client and the
// spawned daemon (both derive it from the same --compdb flag).
var keySalt string

// SetKey sets the compile-DB scope for daemon addressing (call before Query/Serve).
func SetKey(salt string) { keySalt = salt }

func stateDir(root string) string {
	h := sha1.Sum([]byte(root + "\x00" + keySalt))
	base, _ := os.UserCacheDir()
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "ccq", hex.EncodeToString(h[:8]))
}

func addrFile(root string) string { return filepath.Join(stateDir(root), "addr") }

// hasStaticIndex reports whether clangd has persisted a background index for root.
func hasStaticIndex(root string) bool {
	entries, err := os.ReadDir(filepath.Join(root, ".cache", "clangd", "index"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".idx" {
			return true
		}
	}
	return false
}

func revFile(dir string) string { return filepath.Join(dir, "indexed_rev") }

func readRev(dir string) string {
	b, _ := os.ReadFile(revFile(dir))
	return strings.TrimSpace(string(b))
}

func writeRev(dir, rev string) {
	if rev != "" {
		os.WriteFile(revFile(dir), []byte(rev), 0o644)
	}
}

func fileExists(p string) bool { _, e := os.Stat(p); return e == nil }

// indexMode reports how clangd is indexing this project.
func indexMode(root, ccDir string) string {
	if ccDir != "" && fileExists(filepath.Join(ccDir, "compile_commands.json")) {
		return "compile_commands"
	}
	if fileExists(filepath.Join(root, "compile_flags.txt")) {
		return "no-build"
	}
	return "none"
}

// Meta is written into each daemon's state dir so `ccq cache` can show which
// project a hashed dir belongs to.
type Meta struct {
	Root    string `json:"root"`
	CCDir   string `json:"ccDir,omitempty"`
	Mode    string `json:"mode"`
	Files   int    `json:"files"`
	Started string `json:"started"`
}

func writeMeta(dir, root, ccDir, mode string, files int) {
	b, _ := json.Marshal(Meta{root, ccDir, mode, files, time.Now().Format(time.RFC3339)})
	os.WriteFile(filepath.Join(dir, "meta"), b, 0o644)
}

// ReadMeta reads a daemon state dir's meta (for `ccq cache`).
func ReadMeta(dir string) (Meta, bool) {
	b, err := os.ReadFile(filepath.Join(dir, "meta"))
	if err != nil {
		return Meta{}, false
	}
	var m Meta
	if json.Unmarshal(b, &m) != nil {
		return Meta{}, false
	}
	return m, true
}

// CacheBase returns the root of ccq's per-project state (UserCacheDir/ccq).
func CacheBase() string {
	base, _ := os.UserCacheDir()
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "ccq")
}

// Serve runs the daemon: warm clangd, listen, handle requests until idle/shutdown.
func Serve(root, clangdBin, ccDir string, openCap int, maxWait, baseline time.Duration) error {
	dir := stateDir(root)
	os.MkdirAll(dir, 0o755)

	// mark "indexing" until we start listening, so `ccq status` can distinguish
	// background indexing from a stopped daemon.
	marker := filepath.Join(dir, "indexing")
	os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)), 0o644)
	defer os.Remove(marker)

	client, err := lsp.Start(clangdBin, root, ccDir)
	if err != nil {
		return err
	}
	defer client.Close()
	// Warm restart: if clangd already has a static index on disk, prioritise
	// re-indexing the files changed since we last indexed and shorten the wait.
	warm := hasStaticIndex(root)
	var changed []string
	if warm {
		changed = gitdiff.ChangedSince(root, readRev(dir))
		baseline /= 3
	}
	nFiles := 0
	if warm && os.Getenv("CCQ_INCREMENTAL") != "" {
		// Incremental (opt-in): open ONLY changed files and let the persisted
		// static index answer everything else; query-path opens target files on
		// demand. Falls back to one anchor file so workspace/symbol activates.
		if nFiles = client.OpenFiles(changed); nFiles == 0 {
			nFiles = client.OpenAll(root, 1)
		}
	} else {
		// Full (default): prioritise changed files, then open everything for
		// correctness regardless of clangd quirks.
		nFiles = client.OpenFiles(changed) + client.OpenAll(root, openCap)
	}
	client.WaitIndex(maxWait, baseline)
	writeRev(dir, gitdiff.Head(root))
	mode := indexMode(root, ccDir)
	writeMeta(dir, root, ccDir, mode, nFiles) // for `ccq cache` / `ccq status`
	os.Remove(marker)                         // indexing done; we're about to listen = ready

	ln, addr, cleanup, err := listen(dir)
	if err != nil {
		return err
	}
	defer cleanup()
	os.WriteFile(addrFile(root), []byte(addr), 0o644)
	defer os.Remove(addrFile(root))

	var mu sync.Mutex
	last := time.Now()
	// idle watchdog
	go func() {
		for {
			time.Sleep(time.Minute)
			mu.Lock()
			idle := time.Since(last)
			mu.Unlock()
			if idle > idleTimeout {
				ln.Close()
				return
			}
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil // listener closed (idle/shutdown)
		}
		mu.Lock()
		last = time.Now()
		mu.Unlock()
		func() {
			defer conn.Close()
			line, err := bufio.NewReader(conn).ReadBytes('\n')
			if err != nil {
				return
			}
			var rq reqMsg
			json.Unmarshal(line, &rq)
			if rq.Ping {
				// the daemon only listens after WaitIndex, so "reachable" == index ready.
				writeJSON(conn, respMsg{Output: "pong", Mode: mode, Files: nFiles})
				return
			}
			if rq.Shutdown {
				writeJSON(conn, respMsg{Output: "bye"})
				ln.Close()
				return
			}
			var buf strings.Builder
			ctx := &cmd.Ctx{Client: client, Root: root, Out: &buf}
			if !ctx.Dispatch(rq.Request) {
				writeJSON(conn, respMsg{Err: "unknown command: " + rq.Cmd})
				return
			}
			writeJSON(conn, respMsg{Output: buf.String()})
		}()
	}
}

// Query connects to the project's daemon (spawning it if needed) and runs req,
// returning the command's text output.
func Query(root, exe, clangdBin, compdbArg, configArg string, req cmd.Request) (string, error) {
	conn, err := connectOrSpawn(root, exe, clangdBin, compdbArg, configArg)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	writeJSON(conn, reqMsg{Request: req})
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return "", err
	}
	var rp respMsg
	json.Unmarshal(line, &rp)
	if rp.Err != "" {
		return rp.Output, fmt.Errorf("%s", rp.Err)
	}
	return rp.Output, nil
}

// Shutdown stops the daemon for root, if running.
func Shutdown(root string) error {
	conn, err := dial(root)
	if err != nil {
		return nil
	}
	defer conn.Close()
	writeJSON(conn, reqMsg{Shutdown: true})
	bufio.NewReader(conn).ReadBytes('\n')
	return nil
}

func spawnArgs(root, clangdBin, compdbArg, configArg string) []string {
	args := []string{"__daemon", "-p", root}
	if clangdBin != "" {
		args = append(args, "--clangd", clangdBin)
	}
	if compdbArg != "" {
		args = append(args, "--compdb", compdbArg)
	}
	if configArg != "" {
		args = append(args, "--config", configArg)
	}
	return args
}

func connectOrSpawn(root, exe, clangdBin, compdbArg, configArg string) (net.Conn, error) {
	if conn, err := dial(root); err == nil {
		return conn, nil
	}
	c := exec.Command(exe, spawnArgs(root, clangdBin, compdbArg, configArg)...)
	detach(c)
	if err := c.Start(); err != nil {
		return nil, err
	}
	// wait for it to come up (it indexes first — allow generous time)
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		if conn, err := dial(root); err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("daemon did not start in time")
}

// Stat is a daemon's index status (from a Ping).
type Stat struct {
	Running bool
	Mode    string // compile_commands | no-build | none
	Files   int
}

// Status pings the daemon (no spawn) and returns its index status.
func Status(root string) (Stat, error) {
	conn, err := dial(root)
	if err != nil {
		return Stat{}, err
	}
	defer conn.Close()
	writeJSON(conn, reqMsg{Ping: true})
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return Stat{}, err
	}
	var rp respMsg
	json.Unmarshal(line, &rp)
	return Stat{rp.Output == "pong", rp.Mode, rp.Files}, nil
}

// IsIndexing reports whether a daemon is currently doing its initial index for
// root (state dir has a recent "indexing" marker but isn't listening yet).
func IsIndexing(root string) bool {
	fi, err := os.Stat(filepath.Join(stateDir(root), "indexing"))
	return err == nil && time.Since(fi.ModTime()) < 10*time.Minute
}

// EnsureReady spawns the daemon if needed and BLOCKS until it is up (the daemon
// only listens after indexing), then returns its status. Backs `ccq wait-index`.
func EnsureReady(root, exe, clangdBin, compdbArg, configArg string) (Stat, error) {
	conn, err := connectOrSpawn(root, exe, clangdBin, compdbArg, configArg)
	if err != nil {
		return Stat{}, err
	}
	conn.Close()
	return Status(root)
}

// Spawn starts the detached daemon (if not already running) and returns at once
// (does not wait for indexing). Backs `ccq wait-index --background`.
func Spawn(root, exe, clangdBin, compdbArg, configArg string) error {
	if conn, err := dial(root); err == nil {
		conn.Close()
		return nil
	}
	c := exec.Command(exe, spawnArgs(root, clangdBin, compdbArg, configArg)...)
	detach(c)
	return c.Start()
}

func dial(root string) (net.Conn, error) {
	b, err := os.ReadFile(addrFile(root))
	if err != nil {
		return nil, err
	}
	addr := strings.TrimSpace(string(b))
	network := "unix"
	target := strings.TrimPrefix(addr, "unix:")
	if strings.HasPrefix(addr, "tcp:") {
		network = "tcp"
		target = strings.TrimPrefix(addr, "tcp:")
	}
	return net.DialTimeout(network, target, 2*time.Second)
}

func listen(dir string) (ln net.Listener, addr string, cleanup func(), err error) {
	if runtime.GOOS == "windows" {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		if e != nil {
			return nil, "", func() {}, e
		}
		return l, "tcp:" + l.Addr().String(), func() { l.Close() }, nil
	}
	sock := filepath.Join(dir, "sock")
	os.Remove(sock)
	l, e := net.Listen("unix", sock)
	if e != nil {
		return nil, "", func() {}, e
	}
	return l, "unix:" + sock, func() { l.Close(); os.Remove(sock) }, nil
}

func writeJSON(conn net.Conn, v any) {
	b, _ := json.Marshal(v)
	conn.Write(append(b, '\n'))
}
