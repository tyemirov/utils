// Package pointers provides helper functions to obtain pointers to basic
// primitive values. These utilities simplify working with optional or pointer
// based APIs.
package pointers

// FromFloat returns a pointer to the provided float64 value.
func FromFloat(value float64) *float64 {
	return &value
}

// FromString returns a pointer to the provided string value.
func FromString(value string) *string {
	return &value
}

// FromInt returns a pointer to the provided int value.
func FromInt(value int) *int {
	return &value
}

// FromBool returns a pointer to the provided bool value.
func FromBool(value bool) *bool {
	return &value
}
