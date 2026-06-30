/* B: handler is declared extern (no definition in the project). The
   real-function gate drops it -> documented false negative. */
struct bxt { int (*xtop)(int); };
extern int bxt_ext(int x);
static struct bxt BXT = { .xtop = bxt_ext };
int bxt_d(struct bxt *p){ return p->xtop(1); }
