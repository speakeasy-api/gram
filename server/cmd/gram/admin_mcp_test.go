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
		name    string
		key     string
		wantErr bool
	}{
		{name: "missing", wantErr: true},
		{name: "short", key: "short", wantErr: true},
		{name: "reuses admin encryption", key: adminKey, wantErr: true},
		{name: "reuses application encryption", key: applicationKey, wantErr: true},
		{name: "independent", key: strings.Repeat("c", 32)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateAdminMCPSigningKey(tt.key, adminKey, applicationKey)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
