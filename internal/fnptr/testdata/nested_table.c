/* table row with a brace-wrapped scalar in the fn-pointer slot:
   { "g", { gc_a } } — scanRow must recurse into the nested brace. */
struct gcmd { const char *name; int (*fn)(int); };

static int gc_a(int x){ return x + 1; }

static struct gcmd gcmds[] = {
    { "g", { gc_a } },
};

int g_dispatch(struct gcmd *p, int x){ return p->fn(x); }
