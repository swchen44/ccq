/* B/C: NULL and 0 registrations must produce no edge; a real handler still does. */
struct bnl { int (*nla)(void); int (*nlb)(void); int (*nlc)(void); };
static int bnl_real(void){ return 1; }
static struct bnl BNL = { .nla = bnl_real, .nlb = NULL, .nlc = 0 };
int bnl_da(struct bnl *p){ return p->nla(); }
int bnl_db(struct bnl *p){ return p->nlb(); }
int bnl_dc(struct bnl *p){ return p->nlc(); }
