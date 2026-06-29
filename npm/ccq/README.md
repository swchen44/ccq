# @swchen44/ccq

clangd-powered **C/C++** code-intelligence CLI for AI agents — a zero-dependency Go binary,
delivered via npm for convenience.

```bash
npm i -g @swchen44/ccq
ccq init && ccq explore main
```

- **Languages:** C/C++ only (plus Objective-C/CUDA via clangd). Not Rust/Go/Python — those have their own LSP.
- **Requires `clangd`** on your PATH (ccq's one external runtime dependency). Install LLVM/clangd, or pass `--clangd /path/to/clangd`.
- The matching prebuilt binary is installed automatically per platform (darwin/linux x64+arm64, win32 x64) via npm's `os`/`cpu`-gated optional dependencies — no postinstall download.
- **Air-gapped / intranet?** Prefer the plain binary install (copy binary + `clangd`); see the [main repo](https://github.com/swchen44/ccq). npm needs a (possibly private/mirrored) registry.

Full docs, design, benchmarks and case studies: **https://github.com/swchen44/ccq**
