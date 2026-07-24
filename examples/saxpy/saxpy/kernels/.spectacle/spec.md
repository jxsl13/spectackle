---
schema: v0
prefix: CUDA
---

# CUDA kernel contracts

## CUDA-KRN-001
WHEN a kernel launch statement returns, the host wrapper SHALL check cudaGetLastError and propagate its numeric value to the caller.

Rationale: launches fail asynchronously; an unchecked launch hides errors.

## CUDA-KRN-002
The kernel SHALL guard every element access with an explicit index bound check of the form `if (i < n)`.

Rationale: grid size is rounded up to full blocks; overshooting threads must not write.
