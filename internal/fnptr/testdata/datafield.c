struct box { int count; int (*fn)(void); };
static int helper(void){ return 7; }
static struct box B = { .count = 3, .fn = helper };
int total(struct box *x){ return x->count + 1; }
