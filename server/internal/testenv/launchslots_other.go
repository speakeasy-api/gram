//go:build !unix

package testenv

// tryLockFile always succeeds where file locks are unavailable, which leaves
// container launches unbounded.
func tryLockFile(string) (func(), bool, error) {
	return func() {}, true, nil
}
