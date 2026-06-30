/* B: nested struct initializer — inner struct has the fn-pointer field. */
struct bns_inner { int (*nsfn)(int); };
struct bns_outer { int id; struct bns_inner nsin; };
static int bns_h(int x){ return x; }
static struct bns_outer BNS = { .id = 1, .nsin = { .nsfn = bns_h } };
int bns_d(struct bns_outer *p){ return p->nsin.nsfn(5); }
