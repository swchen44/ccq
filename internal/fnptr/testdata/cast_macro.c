/* handler wrapped in a cast or a one-arg macro in the registration slot:
   (scan_fn)cm_scan  and  WRAP(cm_init)  — both should resolve to the real fn. */
#define WRAP(f) (f)
typedef int (*scan_fn)(int);

struct cmops { int (*scan)(int); int (*init)(void); };

static int cm_scan(int x){ return x; }
static int cm_init(void){ return 0; }

static struct cmops CMO = {
    .scan = (scan_fn) cm_scan,
    .init = WRAP(cm_init),
};

int cm_dispatch_scan(struct cmops *p, int x){ return p->scan(x); }
int cm_dispatch_init(struct cmops *p){ return p->init(); }
