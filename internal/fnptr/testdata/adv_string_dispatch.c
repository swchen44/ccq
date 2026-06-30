/* C: a dispatch-like token inside a STRING LITERAL. stripComment removes
   comments but not strings, so this risks a false-positive caller. */
struct cst { int (*cstemit)(void); };
static int cst_real(void){ return 1; }
static struct cst CST = { .cstemit = cst_real };
int cst_realcaller(struct cst *p){ return p->cstemit(); }
const char *cst_doc(void){ return "remember to call p->cstemit() before exit"; }
