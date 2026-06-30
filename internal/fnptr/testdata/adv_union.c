/* B: a union carrying a fn-pointer field. */
union buni { int (*unh)(int); int raw; };
static int buni_h(int x){ return x; }
static union buni BUNI = { .unh = buni_h };
int buni_d(union buni *p){ return p->unh(1); }
