/* D: handler is registered but nothing ever dispatches the field. */
struct dnd { int (*ndgo)(void); };
static int dnd_h(void){ return 1; }
static struct dnd DND = { .ndgo = dnd_h };
