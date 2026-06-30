/* C: a free function whose name equals the fn-pointer field name. Must not be
   confused with the field/handler bridge. */
struct cfn { int (*cfstep)(void); };
static int cfn_h(void){ return 1; }
static struct cfn CFN = { .cfstep = cfn_h };
int cfstep(void){ return 0; }
int cfn_d(struct cfn *p){ return p->cfstep(); }
