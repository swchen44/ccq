/* A: dispatch split across two source lines. */
struct acl { int (*clstep)(void); };
static int acl_h(void){ return 1; }
static struct acl ACL = { .clstep = acl_h };
int acl_caller(struct acl *p){
    return p->
        clstep();
}
