/* A: handler whose RETURN TYPE is on the previous line, so the function name
   sits at column 0 (K&R / BSD / kernel style). The real-function gate must still
   recognize the definition, else the `.sdscan = sd_handler` registration is
   dropped and the dispatch edge is lost. Mirrors wpa_supplicant's
   wpa_driver_bsd_scan / wpa_driver_ndis_scan (missed at 3/5 before this fix). */
struct sdops { int (*sdscan)(int); };

static int
sd_handler(int x)
{
	return x;
}

static const struct sdops SDOPS = { .sdscan = sd_handler };

int sd_dispatch(struct sdops *p){ return p->sdscan(0); }
