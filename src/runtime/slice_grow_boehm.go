//go:build gc.boehm

package runtime

// sliceGrowCap is the capacity for a slice of oldCap that needs newCap, as Go
// grows it: double while small, then by a quarter, see nextslicecap in Go's
// runtime/slice.go. Doubling a large slice left it up to half empty.
func sliceGrowCap(oldCap, newCap, elemSize uintptr) uintptr {
	c := oldCap + oldCap
	const threshold = 256
	switch {
	case newCap > c:
		c = newCap
	case oldCap >= threshold:
		c = oldCap
		for c < newCap {
			next := c + (c+3*threshold)>>2
			if next <= c {
				return newCap
			}
			c = next
		}
	}
	// Fill the 16 byte granule bdwgc rounds a small allocation up to.
	if elemSize > 0 && elemSize < 16 && c < 1<<20 {
		c = (c*elemSize + 15) &^ 15 / elemSize
	}
	return c
}
