# Worked example — a Go call into a CUDA kernel

`examples/saxpy` is the canonical chain spectacle exists for:

```
go:saxpy.Saxpy ──cgo──→ c:launch_saxpy ──launch──→ cu:saxpy_kernel
(saxpy/saxpy.go)        (kernels/saxpy.cu,          (kernels/saxpy.cu,
                         extern "C")                 __global__)
```

## The contracts on disk

`examples/saxpy/.spectacle.ears.md` — API level:

> **SXP-API-001** (N) IF a CUDA wrapper returns a non-zero status, THEN the
> Go binding SHALL wrap the status in an error that contains the numeric
> CUDA status code.
>
> **SXP-API-002** (E) WHEN Saxpy is called with n less than 1 or a slice
> shorter than n, the Go binding SHALL return a non-nil `error` before
> crossing the cgo boundary.

`examples/saxpy/saxpy/kernels/.spectacle.ears.md` — CUDA level:

> **CUDA-KRN-001** (E) WHEN a kernel launch statement returns, the host
> wrapper SHALL check cudaGetLastError and propagate its numeric value to
> the caller.
>
> **CUDA-KRN-002** (U) The kernel SHALL guard every element access with an
> explicit index bound check of the form `if (i < n)`.

The repo-global `SPX-ARC-*` rules cascade in above both files.

## The LLM session (task: "support strided x/y access")

Three tool calls, ~300 result tokens, zero file reads before editing:

```
> sym {"q":"Saxpy"}
n go:saxpy.Saxpy fn examples/saxpy/saxpy/saxpy.go:22 sig=(n int,a float32,x,y []float32)error

> plan_change {"targets":["go:saxpy.Saxpy"],"intent":"support strided x/y access"}
#impact
n go:saxpy.Saxpy fn examples/saxpy/saxpy/saxpy.go:22
e go:saxpy.Saxpy cgo c:launch_saxpy via=examples/saxpy/saxpy/saxpy.go:27
n c:launch_saxpy fn examples/saxpy/saxpy/kernels/saxpy.cu:15
e c:launch_saxpy launch cu:saxpy_kernel via=examples/saxpy/saxpy/kernels/saxpy.cu:29
n cu:saxpy_kernel kernel examples/saxpy/saxpy/kernels/saxpy.cu:7
#contracts
r SPX-ARC-002 - .spectacle WHEN a tool result exceeds the requested token budget, …
r SXP-API-001 N examples/saxpy IF a CUDA wrapper returns a non-zero status, THEN the Go binding SHALL wrap …
r SXP-API-002 E examples/saxpy WHEN Saxpy is called with n less than 1 or a slice shorter than n, …
r CUDA-KRN-001 E examples/saxpy/saxpy/kernels WHEN a kernel launch statement returns, the host wrapper SHALL check cudaGetLastError …
r CUDA-KRN-002 U examples/saxpy/saxpy/kernels The kernel SHALL guard every element access with an explicit index bound check …
#gaps
g none -
```

The agent now knows, without opening a single file:

1. **Where the signature change ripples**: `Saxpy` (Go) → `launch_saxpy`
   (C wrapper, `saxpy.h` + `saxpy.cu`) → `saxpy_kernel` (CUDA) — every edit
   site with file:line.
2. **What every edit must preserve**: bound checks stay (`CUDA-KRN-002`,
   now `i*stride < n`), the launch-error check stays (`CUDA-KRN-001`), host
   validation must cover the new stride params (`SXP-API-002`).

Before editing, the new behaviour is specified — **through the server, never
by hand-writing markdown**. The agent fills the slots (or the server elicits
them from the user via an MCP form); composition, linting, ID assignment and
persistence happen server-side:

```
> add_rule {"dir":"examples/saxpy/saxpy/kernels","pattern":"E",
            "system":"kernel",
            "trigger":"stride parameters are supplied",
            "response":"index x and y as i*stride and guard each access with i*stride < n"}
ok CUDA-KRN-003 examples/saxpy/saxpy/kernels/.spectacle.ears.md
r CUDA-KRN-003 E examples/saxpy/saxpy/kernels WHEN stride parameters are supplied, the kernel SHALL index x and y as i*stride and guard each access with i*stride < n.
```

(A vague response slot — say `"handle strides properly"` — would return
`! E004 … ! REJECTED` and write nothing.) The three files are then edited
consistently (Go signature + validation, `extern "C"` prototype in
`saxpy.h`/`saxpy.cu`, kernel indexing), and the loop closes with:

```
> coverage {"path":"examples/saxpy"}
ok all source directories covered

> link {"rule":"CUDA-KRN-003","id":"cu:saxpy_kernel"}
ok CUDA-KRN-003 -> cu:saxpy_kernel
```

## Why this beats prose + file dumps

- The EARS conditions translate **deterministically**: `CUDA-KRN-001`
  *is* `if ((err = cudaGetLastError()) != cudaSuccess) …` in C and
  `if status != 0 { return fmt.Errorf(…, int(status)) }` in Go. No
  interpretation, no reviewer round trip.
- The cascade loaded **five rules**, not the spec corpus; the impact radius
  named **three edit sites**, not three files of content. That asymmetry —
  structure and contracts in, raw text never — is the token-efficiency
  thesis of the whole server.

*(M0 note: `sym` and `#impact` return stubs until the M1 indexer lands; the
`#contracts`, `lint_ears`, `coverage` and `link` calls above run for real
today — try `plan_change` with `"targets":["examples/saxpy/saxpy/saxpy.go"]`.)*
