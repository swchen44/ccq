package fnptr

import (
	"os"
	"path/filepath"
	"testing"
)

// The testdata dir ships a ccq.fnptr.json with:
//   registrations: io.read += late_io_read
//   links:         event_loop -> io_read

func TestManualLink(t *testing.T) {
	// event_loop reaches io_read via the override "links" entry.
	if !hasCaller(t, "io_read", "event_loop") {
		t.Error("io_read should have manual-link caller event_loop from ccq.fnptr.json")
	}
}

func TestManualRegistration(t *testing.T) {
	// late_io_read is registered to io.read by the override; the io.read dispatch
	// in only_io_reads should therefore reach it.
	if !hasCaller(t, "late_io_read", "only_io_reads") {
		t.Error("late_io_read should be reached from only_io_reads via manual registration io.read")
	}
}

func TestAutoStillWorksWithTable(t *testing.T) {
	// The override must not break the automatic results.
	if !hasCaller(t, "io_read", "only_io_reads") {
		t.Error("auto io.read dispatch should still reach io_read")
	}
	if hasCaller(t, "stream_read", "only_io_reads") {
		t.Error("cross-bleed must still be prevented with a table present")
	}
}

func TestCheckWarnsUnknown(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.c"), []byte("int real(void){return 0;}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ccq.fnptr.json"), []byte(`{
		"links": [ { "from": "real", "to": ["ghost"] } ]
	}`), 0o644)
	found, warnings, err := Check(dir)
	if err != nil || !found {
		t.Fatalf("Check found=%v err=%v", found, err)
	}
	if len(warnings) == 0 {
		t.Error("expected a warning for undefined target 'ghost'")
	}
}

func TestNoTableNoError(t *testing.T) {
	found, _, err := Check(t.TempDir())
	if err != nil || found {
		t.Errorf("empty dir: found=%v err=%v, want (false,nil)", found, err)
	}
}
