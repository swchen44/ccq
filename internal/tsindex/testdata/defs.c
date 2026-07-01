/* general definitions the tree-sitter backend should recover, incl. one behind a
   disabled #ifdef (proving #ifdef-blind) and a K&R column-0 name. */
int active_fn(int x) { return x; }

#ifdef NEVER_DEFINED
int hidden_fn(int y) { return y + 1; }
struct hidden_s { int a; };
#endif

struct active_s { int b; };
union active_u { int i; float f; };
enum colors { RED, GREEN };
typedef struct { int n; } my_t;

static int
kr_name_fn(void *p)   /* return type on the previous line; name at column 0 */
{ return 0; }
