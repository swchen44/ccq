struct io     { int (*read)(void); int (*write)(int); };
struct stream { int (*read)(void); };
static int io_read(void){ return 1; }
static int io_write(int x){ return x; }
static int stream_read(void){ return 2; }
static struct io     IO = { .read = io_read, .write = io_write };
static struct stream ST = { .read = stream_read };
int only_io_reads(struct io *p){ return p->read(); }
