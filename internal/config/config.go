// Package config loads ccq's project/user settings (allow/deny index filters and
// a no-build fallback toggle) and exposes a global file filter used by every part
// of ccq that walks the source tree. Zero-dependency: settings are plain JSON.
//
// Load order (first existing wins): --config <path> > <root>/ccq.json >
// ~/.config/ccq/ccq.json. The client and the spawned daemon both Load with the
// same root + --config, so the filter is consistent across processes.
package config

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Settings is the on-disk schema (ccq.json).
type Settings struct {
	Allow []string `json:"allow"` // if non-empty, index only files matching one
	Deny  []string `json:"deny"`  // files matching any are excluded
}

var (
	loadedRoot string
	source     string // path the settings were loaded from ("" = none/defaults)
	raw        Settings
	allowRe    []*regexp.Regexp
	denyRe     []*regexp.Regexp
	warnings   []string // regex compile errors etc. (surfaced by `ccq config`/`doctor`)
)

// Load reads settings for root (override wins, then root/ccq.json, then user dir).
// It is idempotent per (root, override) and always leaves a usable filter — bad
// regex is reported in Warnings() and treated as "match nothing" (fail-open).
func Load(root, override string) {
	loadedRoot = root
	source, raw, allowRe, denyRe, warnings = "", Settings{}, nil, nil, nil

	p := locate(root, override)
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("%s: %v", p, err))
		return
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		warnings = append(warnings, fmt.Sprintf("%s: invalid JSON: %v", p, err))
		return
	}
	source = p
	allowRe = compile(raw.Allow, "allow")
	denyRe = compile(raw.Deny, "deny")
}

func locate(root, override string) string {
	if override != "" {
		return override
	}
	if p := filepath.Join(root, "ccq.json"); exists(p) {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		if p := filepath.Join(home, ".config", "ccq", "ccq.json"); exists(p) {
			return p
		}
	}
	return ""
}

func compile(pats []string, kind string) []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range pats {
		re, err := regexp.Compile(p)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s regex %q: %v", kind, p, err))
			continue
		}
		out = append(out, re)
	}
	return out
}

// Keep reports whether absPath (a source file) should be indexed under the loaded
// settings. Matching is against the path relative to root (slash-separated). With
// no settings loaded, everything is kept.
func Keep(absPath string) bool {
	if source == "" {
		return true
	}
	rel := absPath
	if r, err := filepath.Rel(loadedRoot, absPath); err == nil && !strings.HasPrefix(r, "..") {
		rel = filepath.ToSlash(r)
	}
	if len(allowRe) > 0 && !matchAny(allowRe, rel) {
		return false
	}
	return !matchAny(denyRe, rel)
}

func matchAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// Source returns the path settings were loaded from, or "" if none.
func Source() string { return source }

// Get returns the loaded settings (for display / fallbackFlags).
func Get() Settings { return raw }

// Warnings returns any load/parse/regex problems (for `ccq config` / `doctor`).
func Warnings() []string { return warnings }

// Key is a stable identity of the active settings (source path + content hash),
// used to scope the warm daemon so a different filter gets a different clangd.
func Key() string {
	if source == "" {
		return ""
	}
	h := sha1.Sum([]byte(strings.Join(raw.Allow, "\x00") + "\x01" + strings.Join(raw.Deny, "\x00")))
	return source + ":" + hex.EncodeToString(h[:4])
}

func exists(p string) bool { _, e := os.Stat(p); return e == nil }
