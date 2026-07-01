/* an iterator/control-flow macro that tree-sitter can't expand -> cascading
   parse failure that drops every function after it (KNOWN LIMITATION). */
int iter_before(void){ return 1; }
static int iter_mid(void *l){
	foreach_thing(x, l, i) {
		use(x);
	};
	return 0;
}
int iter_after(void){ return 2; }
