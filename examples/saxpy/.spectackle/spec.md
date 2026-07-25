---
schema: v1
prefix: SXP
---

# saxpy example — API contracts

## SXP-API-001
IF a CUDA wrapper returns a non-zero status, THEN the Go binding SHALL wrap the status in an error that contains the numeric CUDA status code.

Rationale: Go callers must be able to switch on the code without parsing prose.

## SXP-API-002 {applies: go:saxpy.Saxpy}
WHEN Saxpy is called with n less than 1 or a slice shorter than n, the Go binding SHALL return a non-nil `error` before crossing the cgo boundary.

Rationale: fail fast on the host; never hand invalid extents to the GPU.