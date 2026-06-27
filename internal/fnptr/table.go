package fnptr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Table is a user-provided override that augments the automatic text scan with
// ground-truth associations the heuristic cannot infer — callbacks passed as
// arguments, platform-gated registrations, indirect dispatch, etc. Loaded as
// JSON from the project root (`ccq.fnptr.json`) so it stays zero-dependency.
//
//	{
//	  "registrations": [
//	    { "struct": "wpa_driver_ops", "field": "scan2",
//	      "handlers": ["wpa_driver_bsd_scan", "wpa_driver_ndis_scan"] }
//	  ],
//	  "links": [
//	    { "from": "eloop_run", "to": ["wpa_driver_wext_scan_timeout"], "note": "eloop timer callback" }
//	  ]
//	}
type Table struct {
	Registrations []TableReg  `json:"registrations"`
	Links         []TableLink `json:"links"`
}

// TableReg augments the auto-discovered ops-struct registration map: the named
// fn-pointer field of a struct gains these handlers.
type TableReg struct {
	Struct   string   `json:"struct"`
	Field    string   `json:"field"`
	Handlers []string `json:"handlers"`
}

// TableLink is a direct dispatcher→handler association, for blind spots that
// have no struct/field at all (callbacks, indirect receivers).
type TableLink struct {
	From string   `json:"from"`
	To   []string `json:"to"`
	Note string   `json:"note,omitempty"`
}

// OverridePath, when set (e.g. from --fnptr-table), takes precedence over the
// default file lookup in the project root.
var OverridePath string

var tableNames = []string{"ccq.fnptr.json", filepath.Join(".ccq", "fnptr.json")}

// tablePath returns the override table path for root, or "" if none exists.
func tablePath(root string) string {
	if OverridePath != "" {
		return OverridePath
	}
	for _, n := range tableNames {
		p := filepath.Join(root, n)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// LoadTable reads the override table for root. Returns (nil, "", nil) if none.
func LoadTable(root string) (*Table, string, error) {
	p := tablePath(root)
	if p == "" {
		return nil, "", nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, p, err
	}
	var t Table
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, p, fmt.Errorf("%s: invalid JSON: %w", p, err)
	}
	return &t, p, nil
}

// Check validates the override table against the scanned symbols and returns
// human-readable warnings (e.g. handler names that don't exist). The bool is
// true when a table was found.
func Check(root string) (found bool, warnings []string, err error) {
	t, p, err := LoadTable(root)
	if err != nil {
		return false, nil, err
	}
	if t == nil {
		return false, nil, nil
	}
	ix := build(root) // build also merges the table; funcDefs has all definitions
	known := ix.funcDefs
	warn := func(format string, a ...any) { warnings = append(warnings, fmt.Sprintf(format, a...)) }
	for _, r := range t.Registrations {
		if len(ix.structLayout[r.Struct]) == 0 {
			warn("registration: struct %q not found", r.Struct)
		} else if !structHasField(ix.structLayout[r.Struct], r.Field) {
			warn("registration: %s has no field %q", r.Struct, r.Field)
		}
		for _, h := range r.Handlers {
			if !known[h] {
				warn("registration %s.%s: handler %q not defined in project", r.Struct, r.Field, h)
			}
		}
	}
	for _, l := range t.Links {
		if !known[l.From] {
			warn("link: from %q not defined in project", l.From)
		}
		for _, to := range l.To {
			if !known[to] {
				warn("link from %s: target %q not defined in project", l.From, to)
			}
		}
	}
	_ = p
	return true, warnings, nil
}

func structHasField(fields []fieldInfo, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}
