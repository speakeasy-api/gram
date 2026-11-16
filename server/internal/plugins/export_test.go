package plugins

import "testing"

// VibeProviderSlugForTest is the vibe provider slug the gate checks for.
const VibeProviderSlugForTest = "vibe"

// SetReleasedHooksProvidersForTest overrides the pinned release's provider
// list for the duration of a test, so external test packages can exercise
// both sides of the vibe gate before a release carrying the provider is
// pinned. Tests that call this must not run in parallel with other tests
// that read the list.
func SetReleasedHooksProvidersForTest(t *testing.T, providers []string) {
	t.Helper()
	prev := releasedHooksProviders
	releasedHooksProviders = providers
	t.Cleanup(func() { releasedHooksProviders = prev })
}
