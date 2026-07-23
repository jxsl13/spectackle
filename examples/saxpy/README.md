# saxpy — the worked Go → C → CUDA example

This module demonstrates the cross-language chain spectacle is built for:

```
go:saxpy.Saxpy  --cgo-->  c:launch_saxpy  --launch-->  cu:saxpy_kernel
(saxpy.go)                (saxpy.cu, extern "C")       (saxpy.cu, __global__)
```

and how its behaviour is pinned down by cascading EARS contracts:

- `.spectacle.ears.md` (this directory) — API-level rules (`SXP-…`)
- `saxpy/kernels/.spectacle.ears.md` — CUDA-specific rules (`CUDA-…`)

See `docs/example-go-cuda.md` in the repo root for the full tool-call
transcript an LLM runs against this example.

**Not built by CI.** This is a separate module and requires `nvcc` plus a CUDA
runtime to actually build; it exists as an indexing and spec target, not as a
runnable artifact of the spectacle build.
