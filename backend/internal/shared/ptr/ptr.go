// Package ptr takes the address of a value inside an expression, which Go cannot do with &.
package ptr

// To returns a pointer to a copy of v.
func To[T any](v T) *T { return &v }
