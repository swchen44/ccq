/* B: field registered to another fn-pointer VARIABLE (not a function). The
   real-function gate drops it; ccq must not invent a phantom edge to the var. */
struct bfv { int (*fvop)(void); };
static int bfv_target(void){ return 1; }
static int (*bfv_gvar)(void) = bfv_target;
static struct bfv BFV = { .fvop = bfv_gvar };
int bfv_d(struct bfv *p){ return p->fvop(); }
