//go:build !inv.debug

package inv

// Debug tests a set of invariants and panics if any of them fail. This function
// is only available in "debug" builds when can be enabled by setting any of
// the following build tags: `inv.debug`.
func Debug(group string, pairs ...any) {}
