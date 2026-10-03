package gram

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminMCPSigningKeyConfiguration(t *testing.T) {
	t.Parallel()

	adminKey := strings.Repeat("a", 32)
	applicationKey := strings.Repeat("b", 32)
	for _, tt := range []struct {
		name      string
		key       string
		wantPanic bool
	}{
		{name: "missing", wantPanic: true},
		{name: "short", key: "short", wantPanic: true},
		{name: "reuses admin encryption", key: adminKey, wantPanic: true},
		{name: "reuses application encryption", key: applicationKey, wantPanic: true},
		{name: "independent", key: strings.Repeat("c", 32)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			check := func() { requireAdminMCPSigningKey(tt.key, adminKey, applicationKey) }
			if tt.wantPanic {
				require.Panics(t, check)
			} else {
				require.NotPanics(t, check)
			}
		})
	}
}
