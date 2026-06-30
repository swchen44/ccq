/* A: cast receiver ((struct acr*)v)->op(). */
struct acr { int (*crop)(void); };
static int acr_h(void){ return 1; }
static struct acr ACR = { .crop = acr_h };
int acr_caller(void *v){ return ((struct acr*)v)->crop(); }
