/* Fixtures for the pure-text definition index. The point of this index is to
   see definitions that clangd drops in no-build mode because they sit behind a
   disabled #ifdef — so the scanner must NOT evaluate the preprocessor. */

int active_fn(int x) { return x; }

#ifdef NOT_DEFINED_ANYWHERE
/* hidden behind a config that is never defined: clangd-in-no-build can't see
   this, the text index must. */
int hidden_fn(int y) { return y + 1; }
struct hidden_struct { int a; };
#endif

struct active_struct { int b; };

union active_union { int i; float f; };

enum colors { RED, GREEN, BLUE };

typedef struct { int n; } my_typedef_t;

typedef int my_int_alias;

#define MY_MACRO 42

/* a fake definition inside a comment: int comment_fake(void) { return 0; } */
static const char *s = "int string_fake(void) { return 0; }";

/* function whose opening brace is on the next line */
int nextline_brace_fn(void)
{
	return 7;
}
