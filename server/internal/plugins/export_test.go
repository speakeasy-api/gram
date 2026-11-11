package plugins

import "testing"

// VibeProviderMinHooksBinaryVersionForTest exposes the minimum hooks release
// that carries the vibe provider.
const VibeProviderMinHooksBinaryVersionForTest = vibeProviderMinHooksBinaryVersion

// SetPinnedHooksBinaryVersionForTest overrides the hooks release pin for the
// duration of a test, so external test packages can exercise both sides of
// the vibe gate before the release PR moves the generated pin past
// vibeProviderMinHooksBinaryVersion. Tests that call this must not run in
// parallel with other tests that read the pin.
func SetPinnedHooksBinaryVersionForTest(t *testing.T, version string) {
	t.Helper()
	prev := pinnedHooksBinaryVersion
	pinnedHooksBinaryVersion = version
	t.Cleanup(func() { pinnedHooksBinaryVersion = prev })
}
