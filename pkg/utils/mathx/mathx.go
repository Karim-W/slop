// Package mathx holds small numeric helpers the standard library lacks.
package mathx

import "cmp"

// Clamp limits v to the closed range [lo, hi]. When hi < lo, lo wins.
func Clamp[T cmp.Ordered](v, lo, hi T) T {
	if v > hi {
		v = hi
	}
	if v < lo {
		v = lo
	}
	return v
}

// Fraction returns part/total clamped to [0, 1], or 0 when total is zero.
func Fraction[T ~int | ~int64 | ~uint64 | ~float64](part, total T) float64 {
	if total == 0 {
		return 0
	}
	return Clamp(float64(part)/float64(total), 0, 1)
}
