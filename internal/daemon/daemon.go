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
}

func stateDir(root string) string {
	h := sha1.Sum([]byte(root))
	base, _ := os.UserCacheDir()
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "ccq", hex.EncodeToString(h[:8]))
}

func addrFile(root string) string { return filepath.Join(stateDir(root), "addr") }

// Serve runs the daemon: warm clangd, listen, handle requests until idle/shutdown.
func Serve(root, clangdBin, ccDir string, openCap int, maxWait, baseline time.Duration) error {
	dir := stateDir(root)
	os.MkdirAll(dir, 0o755)

	client, err := lsp.Start(clangdBin, root, ccDir)
	if err != nil {
		return err
	}
	defer client.Close()
	client.OpenAll(root, openCap)
	client.WaitIndex(maxWait, baseline)

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
				writeJSON(conn, respMsg{Output: "pong"})
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
func Query(root, exe, clangdBin string, req cmd.Request) (string, error) {
	conn, err := connectOrSpawn(root, exe, clangdBin)
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

// Ping checks whether a daemon is running for root.
func Ping(root string) (string, error) {
	conn, err := dial(root)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	writeJSON(conn, reqMsg{Ping: true})
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return "", err
	}
	var rp respMsg
	json.Unmarshal(line, &rp)
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

func connectOrSpawn(root, exe, clangdBin string) (net.Conn, error) {
	if conn, err := dial(root); err == nil {
		return conn, nil
	}
	// spawn detached daemon
	args := []string{"__daemon", "-p", root}
	if clangdBin != "" {
		args = append(args, "--clangd", clangdBin)
	}
	c := exec.Command(exe, args...)
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
