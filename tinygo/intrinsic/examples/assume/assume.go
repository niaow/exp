package main

import (
	"fmt"
	"unsafe"

	"github.com/niaow/exp/tinygo/intrinsic"
)

func main() {
	fmt.Println(dotP(vec{1, 2, 3}, vec{4, 5, 6}))
}

type vec []float64

func dotP(x vec, y vec) float64 {
	// tell LLVM that the lengths of x and y are equal
	intrinsic.Assume(len(x) == len(y))
	if intrinsic.ExpectBool(len(x) == 0, false) {
		return 0.0
	}

	// tell the CPU to aggressively load x and y
	// this probbably does not make sense here, but it just shows how to use a prefetch
	intrinsic.Prefetch(unsafe.Pointer(&x[0]), intrinsic.PrefetchRead, intrinsic.MaximumPrefetchLocality, intrinsic.PrefetchData)
	intrinsic.Prefetch(unsafe.Pointer(&y[0]), intrinsic.PrefetchRead, intrinsic.MaximumPrefetchLocality, intrinsic.PrefetchData)

	// compute the dot product
	res := 0.0
	for i := 0; i < len(x); i++ {
		res += x[i] * y[i]
	}

	return res
}
