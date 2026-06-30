/* B: the SAME handler registered to two different (struct,field) keys. */
struct bsh_x { int (*onx)(void); };
struct bsh_y { int (*ony)(void); };
static int bsh_shared(void){ return 1; }
static struct bsh_x BSHX = { .onx = bsh_shared };
static struct bsh_y BSHY = { .ony = bsh_shared };
int bsh_dx(struct bsh_x *p){ return p->onx(); }
int bsh_dy(struct bsh_y *p){ return p->ony(); }
