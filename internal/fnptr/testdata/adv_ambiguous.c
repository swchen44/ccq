/* A/C: field owned by two structs. amb_unknown's receiver type is unresolvable
   (void*), so ccq must report NEITHER handler (no false positive). amb_resolved
   has a concrete type, so only its struct's handler is reported. */
struct amb_a { int (*ampoll)(void); };
struct amb_b { int (*ampoll)(void); };
static int amba_h(void){ return 1; }
static int ambb_h(void){ return 2; }
static struct amb_a AMBA = { .ampoll = amba_h };
static struct amb_b AMBB = { .ampoll = ambb_h };
int amb_unknown(void){
    void *p = pick();
    return p->ampoll();
}
int amb_resolved(struct amb_a *p){ return p->ampoll(); }
