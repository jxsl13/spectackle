// Package saxpy is the Go side of the example Go -> C -> CUDA chain.
package saxpy

/*
#cgo LDFLAGS: -lcudart -L${SRCDIR}/kernels
#include "kernels/saxpy.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Saxpy computes y[i] = a*x[i] + y[i] on the GPU.
//
// Contract chain (see .spectacle.ears.md files):
//
//	SXP-API-001: non-zero CUDA status becomes a Go error carrying the code.
//	CUDA-KRN-001/002: enforced inside kernels/saxpy.cu.
func Saxpy(n int, a float32, x, y []float32) error {
	if n <= 0 || len(x) < n || len(y) < n {
		return fmt.Errorf("saxpy: invalid length n=%d (len(x)=%d len(y)=%d)", n, len(x), len(y))
	}
	status := C.launch_saxpy(
		C.int(n),
		C.float(a),
		(*C.float)(unsafe.Pointer(&x[0])),
		(*C.float)(unsafe.Pointer(&y[0])),
	)
	if status != 0 {
		// SXP-API-001: wrap the numeric CUDA status code.
		return fmt.Errorf("saxpy: CUDA error %d", int(status))
	}
	return nil
}
