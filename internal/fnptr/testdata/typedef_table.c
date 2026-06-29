/* typedef'd (anonymous) struct type used in a positional table, with NO `struct`
   keyword on the initializer — a very common real-world ops/command-table style. */
typedef struct { const char *name; int (*run)(int argc); } tcmd_t;

static int tc_add(int a){ return a + 1; }
static int tc_rm(int a){ return a - 1; }

static tcmd_t tcmds[] = {
    { "add", tc_add },
    { "rm",  tc_rm  },
};

int t_dispatch(tcmd_t *p, int argc){ return p->run(argc); }
