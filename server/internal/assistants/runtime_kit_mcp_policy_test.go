package assistants

import (
	"errors"
	"testing"
)

func TestKitMCPStartupDesiredSetPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                         string
		resuming, provided, rejected bool
	}{
		{"new omitted", false, false, false},
		{"new initial servers including empty", false, true, false},
		{"resident omitted retains", true, false, false},
		{"resume rejects supplied desired set", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateKitMCPStartup(tc.resuming, tc.provided)
			if (!tc.rejected && err != nil) || (tc.rejected && !errors.Is(err, ErrKitMCPDesiredSetUnsupported)) {
				t.Fatalf("unexpected policy result: %v", err)
			}
		})
	}
}
