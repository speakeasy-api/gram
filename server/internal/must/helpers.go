package must

// Nil panics if err is non-nil.
func Nil(err error) {
	if err != nil {
		panic(err)
	}
}

// Value returns v if err is nil, otherwise panics.
func Value[T any](v T, err error) T {
	Nil(err)
	return v
}
