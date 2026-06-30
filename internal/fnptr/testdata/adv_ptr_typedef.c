/* A: recv is a POINTER TYPEDEF (typedef struct s *alias;). Combined with a
   second struct owning the same field so the single-owner fallback can't mask
   a recvType failure. */
struct apt_s { int (*ptrun)(void); };
struct apt_o { int (*ptrun)(void); };
typedef struct apt_s *apt_ptr;
static int apt_h(void){ return 1; }
static int apto_h(void){ return 2; }
static struct apt_s APT = { .ptrun = apt_h };
static struct apt_o APO = { .ptrun = apto_h };
int apt_caller(apt_ptr p){ return p->ptrun(); }
