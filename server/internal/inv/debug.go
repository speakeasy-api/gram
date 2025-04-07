//go:build inv.debug

package inv

// Debug tests a set of invariants and panics if any of them fail.
//
//go:noinline
func Debug(group string, pairs ...any) {
	if err := Check(group, pairs...); err != nil {
		panic(err)
	}
}
