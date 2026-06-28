package fnptr

import "testing"

const dir = "testdata"

// hasCaller reports whether handler has a synthesized caller named fn.
func hasCaller(t *testing.T, handler, fn string) bool {
	t.Helper()
	for _, c := range Callers(dir, handler) {
		if c.Func == fn {
			return true
		}
	}
	return false
}

// TestCrossbleed: a handler registered to struct io's `read` field must NOT be
// reported as called from a dispatch on a different struct's `read` field.
func TestCrossbleed(t *testing.T) {
	if !hasCaller(t, "io_read", "only_io_reads") {
		t.Error("io_read should be reached from only_io_reads (io.read dispatch)")
	}
	if hasCaller(t, "stream_read", "only_io_reads") {
		t.Error("stream_read must NOT be reached from only_io_reads (cross-bleed across structs)")
	}
}

// TestPositionalTable: positional initializer { "add", cmd_add } binds cmd_add
// to the fn-pointer field by index.
func TestPositionalTable(t *testing.T) {
	if !hasCaller(t, "cmd_add", "run_builtin") {
		t.Error("cmd_add should be reached from run_builtin via positional table cmd.fn")
	}
	if !hasCaller(t, "cmd_rm", "run_builtin") {
		t.Error("cmd_rm should be reached from run_builtin via positional table cmd.fn")
	}
}

// TestFieldPropagation: h->func = found->fn must carry registry's handlers into
// hooks.func so the dispatch h->func() reaches them.
func TestFieldPropagation(t *testing.T) {
	if !hasCaller(t, "hk_a", "call") {
		t.Error("hk_a should be reached from call via field<-field propagation (hooks.func <- entry.fn)")
	}
}

// TestDataFieldNotBridged: a plain data field (count) must never produce edges,
// and a fn-pointer field with no dispatch site produces none either.
func TestDataFieldNotBridged(t *testing.T) {
	if hasCaller(t, "helper", "total") {
		t.Error("helper must NOT be reached from total: box.count is data and box.fn is not dispatched")
	}
}

// TestKeyIsStructDotField: synthesized callers carry a "struct.field" key.
func TestKeyIsStructDotField(t *testing.T) {
	got := Callers(dir, "io_read")
	if len(got) == 0 {
		t.Fatal("expected at least one caller for io_read")
	}
	if got[0].Field != "io.read" {
		t.Errorf("Field = %q, want io.read (composite struct.field key)", got[0].Field)
	}
}

// TestCalleesReverse: run_builtin dispatches via cmd.fn, so its fn-pointer
// callees are the registered handlers cmd_add and cmd_rm.
func TestCalleesReverse(t *testing.T) {
	got := Callees(dir, "run_builtin")
	want := map[string]bool{"cmd_add": true, "cmd_rm": true}
	for w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
			}
		}
		if !found {
			t.Errorf("Callees(run_builtin) missing %q; got %v", w, got)
		}
	}
}

// TestBuildCachedAndInvalidate is the regression test for bug #3 (fnptr.build
// rescanned the whole repo on every query). build must return the same cached
// *index for the same root, and Invalidate must force a fresh build.
func TestBuildCachedAndInvalidate(t *testing.T) {
	Invalidate()
	a := build(dir)
	b := build(dir)
	if a != b {
		t.Error("build should return the cached *index on repeated calls (same root)")
	}
	Invalidate()
	if build(dir) == a {
		t.Error("Invalidate should force a fresh build (new *index)")
	}
}

// TestUnknownHandlerNoPanic: a handler that is registered nowhere yields nil.
func TestUnknownHandlerNoPanic(t *testing.T) {
	if c := Callers(dir, "does_not_exist"); c != nil {
		t.Errorf("unknown handler should yield nil, got %v", c)
	}
}
