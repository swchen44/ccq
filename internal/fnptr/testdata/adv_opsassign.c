/* A: an ops field registered via an IMPERATIVE assignment `obj.field = handler;`
   (a statement, not a brace initializer) — the Windows-driver / dynamically-built
   vtable pattern. Mirrors wpa_supplicant's
   `wpa_driver_ndis_ops.scan2 = wpa_driver_ndis_scan;`. */
struct oaops { int (*oascan)(int); };
static int oa_handler(int x){ return x; }
static struct oaops OAOPS;
void oa_register(void){ OAOPS.oascan = oa_handler; }
int oa_dispatch(struct oaops *p){ return p->oascan(0); }
