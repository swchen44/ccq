/* B: designated array index [2] = { ... } in a table initializer. */
struct bai { const char *name; int (*bairun)(int); };
static int bai_h(int x){ return x; }
static struct bai BAI[] = { [2] = { "x", bai_h } };
int bai_d(struct bai *p){ return p->bairun(0); }
