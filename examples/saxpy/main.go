// Command saxpy runs the example single-precision a*x+y computation on the
// GPU via the cgo -> CUDA chain in package saxpy.
package main

import (
	"fmt"
	"log"

	"example.com/saxpy/saxpy"
)

func main() {
	const n = 1 << 20
	x := make([]float32, n)
	y := make([]float32, n)
	for i := range x {
		x[i] = float32(i)
		y[i] = 1
	}
	if err := saxpy.Saxpy(n, 2.0, x, y); err != nil {
		log.Fatal(err)
	}
	fmt.Println("y[42] =", y[42]) // 2*42 + 1
}
