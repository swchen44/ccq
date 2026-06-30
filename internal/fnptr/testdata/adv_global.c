/* A: recv type from a GLOBAL variable, dispatched with `.`. */
struct agl { int (*gtick)(void); };
static int agl_h(void){ return 1; }
static struct agl AGL = { .gtick = agl_h };
int agl_caller(void){ return AGL.gtick(); }
