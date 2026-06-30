/* A: chained member dispatch p->inner->leaf(). */
struct achi { int (*chleaf)(void); };
struct acho { struct achi *chinner; };
static int achi_h(void){ return 1; }
static struct achi ACHI = { .chleaf = achi_h };
int ach_caller(struct acho *p){ return p->chinner->chleaf(); }
