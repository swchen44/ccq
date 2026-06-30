/* B: CYCLIC field<-field propagation must converge (not explode) and still
   carry the handler to the dispatched field. */
struct pcy_a { int (*cfa)(void); };
struct pcy_b { int (*cfb)(void); };
static int pcy_h(void){ return 1; }
static struct pcy_a PCYA = { .cfa = pcy_h };
void pcy_link(struct pcy_a *a, struct pcy_b *b){
    b->cfb = a->cfa;
    a->cfa = b->cfb;
}
int pcy_call(struct pcy_b *b){ return b->cfb(); }
