package inv

// Require tests a set of invariants and panics if any of them fail.
//
//go:noinline
func Require(group string, pairs ...any) {
	if err := Check(group, pairs...); err != nil {
		panic(err)
	}
}
