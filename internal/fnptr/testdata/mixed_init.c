/* designated field FOLLOWED BY a positional entry: after `.a = ...`, the next
   positional value initializes the NEXT field (b), not field 0 again. */
struct mxd { int (*a)(int); int (*b)(int); };

static int mxd_a(int x){ return x; }
static int mxd_b(int x){ return x; }

static struct mxd MXD = { .a = mxd_a, mxd_b };

int mxd_dispa(struct mxd *p, int x){ return p->a(x); }
int mxd_dispb(struct mxd *p, int x){ return p->b(x); }
