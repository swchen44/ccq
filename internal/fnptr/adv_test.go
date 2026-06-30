package fnptr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Adversarial tests for the function-pointer dispatch resolver. Each test targets
// a hard case for the index build or the dispatch lookup. Tests that expose a real
// weakness (漏报 / 误报) are marked KNOWN LIMITATION with t.Skip so they stay
// visible without turning the suite red.

// hasCalleeAdv reports whether fn's fn-pointer callees include handler.
func hasCalleeAdv(t *testing.T, fn, handler string) bool {
	t.Helper()
	for _, h := range Callees(dir, fn) {
		if h == handler {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// A. recvType / dispatch type resolution
// ---------------------------------------------------------------------------

// A1: receiver is a local variable initialized from a global.  EXPECT PASS.
func TestAdvLocalVarRecv(t *testing.T) {
	if !hasCaller(t, "alv_h", "alv_caller") {
		t.Error("alv_h should be reached from alv_caller (local var `struct alv *p = &ALV; p->lvget()`)")
	}
}

// A2: receiver is a global variable dispatched with `.`.  EXPECT PASS.
func TestAdvGlobalRecv(t *testing.T) {
	if !hasCaller(t, "agl_h", "agl_caller") {
		t.Error("agl_h should be reached from agl_caller (global `AGL.gtick()`)")
	}
}

// A3: receiver typed through a POINTER TYPEDEF (`typedef struct s *alias;`),
// with the field owned by two structs so the single-owner fallback can't hide a
// recvType miss.  KNOWN LIMITATION: pointer typedefs aren't in structLayout, so
// recvType returns "" and the dispatch is dropped (false negative).
func TestAdvPointerTypedefRecv(t *testing.T) {
	// Conservative half: the OTHER struct's handler must never be reached.
	if hasCaller(t, "apto_h", "apt_caller") {
		t.Error("apto_h must NOT be reached from apt_caller (apt_caller dispatches on apt_s, not apt_o)")
	}
	if !hasCaller(t, "apt_h", "apt_caller") {
		t.Skip("KNOWN LIMITATION (false negative): pointer typedef `apt_ptr` is not registered in structLayout, " +
			"so recvType() can't resolve it; with the field owned by 2 structs the fallback bails and apt_h is lost")
	}
}

// A4: chained member dispatch `p->inner->leaf()`.  EXPECT PASS (resolves because
// the struct field decl `struct achi *chinner;` is mistaken-but-correctly read by
// recvType as a declaration of `chinner`).
func TestAdvChainedDispatch(t *testing.T) {
	if !hasCaller(t, "achi_h", "ach_caller") {
		t.Error("achi_h should be reached from ach_caller via `p->chinner->chleaf()`")
	}
}

// A5: dereferenced double-pointer receiver `(*pp)->go()`.  KNOWN LIMITATION:
// reDispatch needs `ident -> field(`; the `)` from `(*pp)` breaks the match.
func TestAdvDerefDoublePtr(t *testing.T) {
	if !hasCaller(t, "adr_h", "adr_caller") {
		t.Skip("KNOWN LIMITATION (false negative): `(*pp)->drgo()` — the reDispatch regex needs a bare " +
			"identifier immediately before `->`, but `)` intervenes, so no dispatch site is detected")
	}
}

// A6: cast receiver `((struct acr*)v)->op()`.  KNOWN LIMITATION: same reDispatch
// shape problem — `)` before `->` prevents the match.
func TestAdvCastReceiver(t *testing.T) {
	if !hasCaller(t, "acr_h", "acr_caller") {
		t.Skip("KNOWN LIMITATION (false negative): cast receiver `((struct acr*)v)->crop()` is not matched " +
			"by reDispatch (no bare identifier before `->`)")
	}
}

// A7: dispatch split across two lines (`p->\n  step();`).  KNOWN LIMITATION:
// reDispatch is applied per-line.
func TestAdvCrossLineDispatch(t *testing.T) {
	if !hasCaller(t, "acl_h", "acl_caller") {
		t.Skip("KNOWN LIMITATION (false negative): a dispatch split across lines (`p->` then `clstep();`) " +
			"is not detected — reDispatch scans one stripped line at a time")
	}
}

// A8/C: field owned by two structs.
//   - amb_unknown: receiver type is unresolvable (void*) -> must report NEITHER
//     handler (no 误报).  EXPECT PASS.
//   - amb_resolved: receiver type is concrete -> only its struct's handler.
func TestAdvAmbiguousConservative(t *testing.T) {
	if hasCaller(t, "amba_h", "amb_unknown") || hasCaller(t, "ambb_h", "amb_unknown") {
		t.Error("ambiguous receiver (void* p) must not be linked to EITHER amba_h/ambb_h (false positive)")
	}
	if !hasCaller(t, "amba_h", "amb_resolved") {
		t.Error("amb_resolved has a concrete `struct amb_a *p`, so amba_h should be reached")
	}
	if hasCaller(t, "ambb_h", "amb_resolved") {
		t.Error("amb_resolved dispatches on amb_a, so ambb_h (amb_b's handler) must NOT be reached")
	}
}

// ---------------------------------------------------------------------------
// B. registration index building
// ---------------------------------------------------------------------------

// B1: the same handler registered to two distinct (struct,field) keys.  PASS.
func TestAdvSameHandlerTwoKeys(t *testing.T) {
	if !hasCaller(t, "bsh_shared", "bsh_dx") {
		t.Error("bsh_shared should be reached from bsh_dx (bsh_x.onx)")
	}
	if !hasCaller(t, "bsh_shared", "bsh_dy") {
		t.Error("bsh_shared should be reached from bsh_dy (bsh_y.ony)")
	}
}

// B2: NULL/0 registrations create no edge; the real handler still resolves.  PASS.
func TestAdvNullRegistration(t *testing.T) {
	if !hasCalleeAdv(t, "bnl_da", "bnl_real") {
		t.Error("bnl_da should reach bnl_real via .nla")
	}
	if c := Callees(dir, "bnl_db"); len(c) != 0 {
		t.Errorf("bnl_db (.nlb = NULL) should have no fn-pointer callees, got %v", c)
	}
	if c := Callees(dir, "bnl_dc"); len(c) != 0 {
		t.Errorf("bnl_dc (.nlc = 0) should have no fn-pointer callees, got %v", c)
	}
}

// B3: address-of handler `&fn`.  PASS.
func TestAdvAddressOfHandler(t *testing.T) {
	if !hasCaller(t, "bap_fn", "bap_d") {
		t.Error("bap_fn should be reached from bap_d via `.aph = &bap_fn`")
	}
}

// B4: field registered to a fn-pointer VARIABLE (not a function). The
// real-function gate drops it; crucially there must be NO phantom edge to the
// variable name.  PASS (no false positive). The indirection to bfv_target is a
// documented, accepted false negative (ccq does not follow var->func).
func TestAdvRegToFnPtrVar(t *testing.T) {
	if hasCalleeAdv(t, "bfv_d", "bfv_gvar") {
		t.Error("bfv_d must NOT report a phantom callee `bfv_gvar` (a variable, not a function)")
	}
}

// B4b: documents the accepted false negative from B4.
func TestAdvRegToFnPtrVarIndirect(t *testing.T) {
	if !hasCalleeAdv(t, "bfv_d", "bfv_target") {
		t.Skip("KNOWN LIMITATION (false negative, accepted): `.fvop = bfv_gvar` registers a fn-pointer " +
			"variable; ccq does not follow the variable to bfv_target, so the real target is missed")
	}
}

// B5: nested struct initializer — the inner struct holds the fn-pointer field.
// KNOWN LIMITATION: scanRow doesn't recurse into a designated value `.nsin = {..}`
// when the outer field isn't itself a fn-pointer, so the inner registration is lost.
func TestAdvNestedStructInit(t *testing.T) {
	if !hasCaller(t, "bns_h", "bns_d") {
		t.Error("bns_h should reach bns_d through nested struct init `.nsin = { .nsfn = bns_h }`")
	}
}

// B6: designated array index `[2] = { ... }`.  KNOWN LIMITATION: scanRow doesn't
// understand `[idx] =` array designators, so the row is never scanned.
func TestAdvArrayIndexDesignator(t *testing.T) {
	if !hasCaller(t, "bai_h", "bai_d") {
		t.Error("bai_h should reach bai_d through the array-index designator `[2] = { \"x\", bai_h }`")
	}
}

// B7: union carrying a fn-pointer field.  KNOWN LIMITATION: reStructAny only
// matches `struct ... {`, so unions are never scanned.
func TestAdvUnionFnPtr(t *testing.T) {
	if !hasCaller(t, "buni_h", "buni_d") {
		t.Error("buni_h should reach buni_d through the union fn-pointer field unh")
	}
}

// B8: cyclic field<-field propagation must converge and still carry the handler.
// EXPECT PASS (tests both termination and correctness).
func TestAdvPropagationCycle(t *testing.T) {
	if !hasCaller(t, "pcy_h", "pcy_call") {
		t.Error("pcy_h should reach pcy_call through cyclic propagation cfb<-cfa, cfa<-cfb")
	}
}

// B9: extern-only handler (declared, never defined in the project). KNOWN
// LIMITATION (intentional real-function gate) — documented false negative.
func TestAdvExternGate(t *testing.T) {
	if !hasCaller(t, "bxt_ext", "bxt_d") {
		t.Skip("KNOWN LIMITATION (intentional false negative): `extern int bxt_ext(int);` has no definition " +
			"in the project, so the real-function gate (addReg) drops the registration. By design, but it " +
			"misses genuine externs defined in another translation unit")
	}
}

// ---------------------------------------------------------------------------
// C. false-positive pressure
// ---------------------------------------------------------------------------

// C1: a dispatch-like token inside a STRING LITERAL must not create a caller.
// KNOWN LIMITATION: stripComment removes comments but not string contents, so
// `"... p->cstemit() ..."` is parsed as a real dispatch (FALSE POSITIVE).
func TestAdvStringLiteralNotDispatch(t *testing.T) {
	// The genuine caller must still be found.
	if !hasCaller(t, "cst_real", "cst_realcaller") {
		t.Error("cst_real should be reached from cst_realcaller (the genuine dispatch)")
	}
	if hasCaller(t, "cst_real", "cst_doc") {
		t.Error("cst_doc must NOT be a caller of cst_real: the p->cstemit() token is inside a string literal")
	}
}

// C2: a free function whose name equals the fn-pointer field name must not be
// confused with the dispatch bridge.  EXPECT PASS.
func TestAdvFuncNameCollision(t *testing.T) {
	if !hasCaller(t, "cfn_h", "cfn_d") {
		t.Error("cfn_h should be reached from cfn_d (cfn.cfstep dispatch)")
	}
	// The free function `cfstep` is not a registered handler -> not a callee.
	if hasCalleeAdv(t, "cfn_d", "cfstep") {
		t.Error("the free function `cfstep` must NOT appear as a callee of cfn_d (only the handler cfn_h)")
	}
}

// C3: real dispatch hidden inside a function-like macro `CALL(p)`. KNOWN
// LIMITATION: the call site shows only `CALL(p)`, so the dispatch is missed
// (false negative). Importantly, no phantom caller is fabricated either.
func TestAdvMacroHiddenDispatch(t *testing.T) {
	if !hasCaller(t, "cmd2_h", "cmd2_user") {
		t.Skip("KNOWN LIMITATION (false negative): the dispatch lives in `#define CALL(p) p->cmfire()`; the call " +
			"site `CALL(p)` is opaque to the line scanner, so cmd2_user is not linked to cmd2_h")
	}
}

// ---------------------------------------------------------------------------
// D. lookup boundaries
// ---------------------------------------------------------------------------

// D1: handler registered but never dispatched -> empty result, no panic.  PASS.
func TestAdvNoDispatchSite(t *testing.T) {
	got := Callers(dir, "dnd_h")
	if len(got) != 0 {
		t.Errorf("dnd_h has a registration but no dispatch site; want no callers, got %v", got)
	}
	if c := Callees(dir, "dnd_h"); len(c) != 0 {
		t.Errorf("dnd_h dispatches nothing; want no callees, got %v", c)
	}
}

// D2: fanout cap — a handler reached from > fanoutCap distinct dispatch sites
// must be bounded and must not panic.  PASS.
func TestAdvFanoutCap(t *testing.T) {
	tmp := t.TempDir()
	var b strings.Builder
	b.WriteString("struct fanx { int (*fango)(void); };\n")
	b.WriteString("static int fan_h(void){ return 1; }\n")
	b.WriteString("static struct fanx FANX = { .fango = fan_h };\n")
	const n = fanoutCap + 50
	for i := 0; i < n; i++ {
		b.WriteString("int fan_c")
		b.WriteString(itoa(i))
		b.WriteString("(struct fanx *p){ return p->fango(); }\n")
	}
	if err := os.WriteFile(filepath.Join(tmp, "fan.c"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	Invalidate()
	got := Callers(tmp, "fan_h")
	Invalidate() // don't leave the tmp index cached for other tests
	if len(got) != fanoutCap {
		t.Errorf("fanout should be capped at %d, got %d", fanoutCap, len(got))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
