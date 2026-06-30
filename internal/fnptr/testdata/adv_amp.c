/* B: address-of handler &fn in the registration slot. */
struct bap { int (*aph)(void); };
static int bap_fn(void){ return 1; }
static struct bap BAP = { .aph = &bap_fn };
int bap_d(struct bap *p){ return p->aph(); }
