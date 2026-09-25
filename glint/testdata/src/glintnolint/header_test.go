//nolint:glint // transaction contains only package APIs and SQLc-generated queries // want `move //nolint:glint from the file header` `start the //nolint:glint explanation with the suppressed glint analyzer name\(s\) and a colon`
package glintnolint

import "testing"

func TestSomething(t *testing.T) {
	//nolint:glint // notestingrawsql: own-line directive scoped to the next statement
	helper(
		t.Name(),
	)

	helper(t.Name()) //nolint:glint // prose explanation in a test file // want `start the //nolint:glint explanation`
}

//nolint:glint // notestingrawsql: whole test function suppression // want `move //nolint:glint off top-level func "TestOther"`
func TestOther(t *testing.T) {
	helper(t.Name())
}
