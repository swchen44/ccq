/* A: recv type from a LOCAL variable with initializer. */
struct alv { int (*lvget)(void); };
static int alv_h(void){ return 1; }
static struct alv ALV = { .lvget = alv_h };
int alv_caller(void){
    struct alv *p = &ALV;
    return p->lvget();
}
