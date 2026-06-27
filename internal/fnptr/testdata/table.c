struct cmd { const char *name; int (*fn)(int argc); };
static int cmd_add(int a){ return a+1; }
static int cmd_rm(int a){ return a-1; }
static struct cmd commands[] = { { "add", cmd_add }, { "rm", cmd_rm } };
int run_builtin(struct cmd *p, int argc){ return p->fn(argc); }
