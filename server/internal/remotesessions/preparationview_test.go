package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparationAPIViewPreservesGrantEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		grants []string
	}{
		{"unknown", nil},
		{"empty", []string{}},
		{"populated", []string{PreparationJWTBearerGrant}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			view := preparationAPIView(&PreparationResult{GrantTypes: tc.grants})
			require.Equal(t, tc.grants, view.GrantTypes)
		})
	}
}
