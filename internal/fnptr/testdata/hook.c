typedef int (*hook_func)(void);
struct entry { const char *name; hook_func fn; };
struct hooks { hook_func func; };
static int hk_a(void){ return 1; }
static struct entry registry[] = { { "a", hk_a } };
int call(struct hooks *h, struct entry *found){ h->func = found->fn; return h->func(); }
