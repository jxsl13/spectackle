# Worked example — a Go call into a CUDA kernel

`examples/saxpy` is the canonical chain spectacle exists for:

```
go:saxpy.Saxpy ──cgo──→ c:launch_saxpy ──launch──→ cu:saxpy_kernel
(saxpy/saxpy.go)        (kernels/saxpy.cu,          (kernels/saxpy.cu,
                         extern "C")                 __global__)
```

## The contracts on disk

`examples/saxpy/.spectacle/spec.md` — API level (`SXP-API-001/002`: non-zero
CUDA status becomes a Go error with the numeric code; invalid extents fail
before the cgo boundary). `examples/saxpy/saxpy/kernels/.spectacle/spec.md` —
CUDA level (`CUDA-KRN-001`: check cudaGetLastError after every launch;
`CUDA-KRN-002`: every element access bound-checked). The repo-root `SPX-*`
rules cascade in above both.

## The LLM session (task: "support strided x/y access")

The full lifecycle in six tool calls, ~400 result tokens, zero file reads
before editing:

```
> find {"q":"stride saxpy","scope":"rejection"}          # learn first
ok no matches

> find {"q":"Saxpy","scope":"code"}
n go:saxpy.Saxpy fn examples/saxpy/saxpy/saxpy.go:22 sig=(n int,a float32,x,y []float32)error

> draft {"kind":"proposal","title":"strided saxpy access",
         "body":"Add xStride/yStride params through the whole chain.",
         "targets":["go:saxpy.Saxpy"]}
i P-0001 proposal draft examples/saxpy strided saxpy access
#impact
n go:saxpy.Saxpy fn examples/saxpy/saxpy/saxpy.go:22
e go:saxpy.Saxpy cgo c:launch_saxpy via=examples/saxpy/saxpy/saxpy.go:27
n c:launch_saxpy fn examples/saxpy/saxpy/kernels/saxpy.cu:15
e c:launch_saxpy launch cu:saxpy_kernel via=examples/saxpy/saxpy/kernels/saxpy.cu:29
n cu:saxpy_kernel kernel examples/saxpy/saxpy/kernels/saxpy.cu:7
#contracts
r SXP-API-001 N examples/saxpy IF a CUDA wrapper returns a non-zero status, THEN the Go binding SHALL wrap …
r SXP-API-002 E examples/saxpy WHEN Saxpy is called with n less than 1 …
r CUDA-KRN-001 E examples/saxpy/saxpy/kernels WHEN a kernel launch statement returns, …
r CUDA-KRN-002 U examples/saxpy/saxpy/kernels The kernel SHALL guard every element access …
#rejections
ok none similar
```

The agent now knows — without opening a single file — **where** the change
ripples (three edit sites, file:line) and **what every edit must preserve**
(bound checks stay, launch-error check stays, host validation must cover the
new stride params).

After the user approves, the new behaviour is specified **through the
server** (slots → composed EARS → lint gate → auto-ID; a vague response slot
would return `! E004 … ! REJECTED` and write nothing):

```
> move {"id":"P-0001","to":"submitted"}   → move {"id":"P-0001","to":"approved"} → active

> rule {"op":"add","dir":"examples/saxpy/saxpy/kernels","pattern":"E",
        "system":"kernel","trigger":"stride parameters are supplied",
        "response":"index x and y as i*stride and guard each access with i*stride < n",
        "applies":["cu:saxpy_kernel"],"item":"P-0001"}
ok CUDA-KRN-003 examples/saxpy/saxpy/kernels/.spectacle/spec.md
r CUDA-KRN-003 E examples/saxpy/saxpy/kernels WHEN stride parameters are supplied, the kernel SHALL index x and y as i*stride and guard each access with i*stride < n.
a CUDA-KRN-003 cu:saxpy_kernel examples/saxpy/saxpy/kernels/saxpy.cu:7-13 3fa1b2c4d5e6f708
```

The agent edits all three files consistently (Go signature + validation,
`extern "C"` prototype, kernel indexing), then closes the loop:

```
> check {}
ok                                        # no drift, no gaps, specs lint clean

> move {"id":"P-0001","to":"done"}  →  move {"id":"P-0001","to":"archived"}
i P-0001 proposal archived examples/saxpy strided saxpy access
```

Archive merged the outcome into `examples/saxpy/.spectacle/spec.md ## intent`
and removed the item from work.md; the journal keeps the full history. Had a
later refactor changed the kernel without touching the spec, `check` would
report `d changed CUDA-KRN-003 cu:saxpy_kernel …` and `check fix=true` would
draft the backprop proposal.

## Why this beats prose + file dumps

- EARS conditions translate **deterministically**: `CUDA-KRN-001` *is*
  `if ((err = cudaGetLastError()) != cudaSuccess) …` in C and an error-wrap
  branch in Go. No interpretation, no rework loop.
- The cascade loaded **four rules**, the radius named **three edit sites** —
  structure and contracts in, raw text never.
- The rejection search up front and the drift check at the end are what keep
  this loop from ever doing the same failed work twice.

*(Graph-backed records — `n`/`e` lines and anchor spans — are live as of the
M1 indexer, alongside the lifecycle, contracts, rejection corpus and
check/compact mechanics above.)*
