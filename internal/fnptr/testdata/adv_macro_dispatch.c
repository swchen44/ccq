/* C: the real dispatch is hidden inside a function-like macro; the call site
   only shows CALL(p). */
#define CALL(p) p->cmfire()
struct cmd2 { int (*cmfire)(void); };
static int cmd2_h(void){ return 1; }
static struct cmd2 CMD2 = { .cmfire = cmd2_h };
int cmd2_user(struct cmd2 *p){ return CALL(p); }
