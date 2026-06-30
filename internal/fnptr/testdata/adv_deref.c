/* A: dereferenced double-pointer receiver (*pp)->go(). */
struct adr { int (*drgo)(void); };
static int adr_h(void){ return 1; }
static struct adr ADR = { .drgo = adr_h };
int adr_caller(struct adr **pp){ return (*pp)->drgo(); }
