/* comments that must not corrupt parsing:
   - a multi-line block comment containing commas and braces inside the initializer
   - a // line comment with commas
   The .scan registration must still resolve to ml_scan. */
struct mlops {
    int (*scan)(int);    /* multi
                            line */
    int (*deinit)(void);
};

static int ml_scan(int x){ return x; }
static int ml_deinit(void){ return 0; }

static struct mlops MLO = {
    /* a faux row: { not, a, real, entry } */
    .scan = ml_scan,   // sets scan, e.g. a, b, c
    .deinit = ml_deinit,
};

int ml_dispatch(struct mlops *p, int x){ return p->scan(x); }
