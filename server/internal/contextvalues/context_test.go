package contextvalues

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSupportSessionRequiresValidatedContext(t *testing.T) {
	t.Parallel()
	base := &AuthContext{ActiveOrganizationID: "org_123", IsAdmin: true, SupportOrganizationID: "org_123"}
	require.False(t, IsSupportSession(SetAuthContext(t.Context(), base)))
	require.True(t, IsSupportSession(WithValidatedSupportSession(t.Context(), base)))

	mismatch := *base
	mismatch.SupportOrganizationID = "org_other"
	require.False(t, IsSupportSession(WithValidatedSupportSession(t.Context(), &mismatch)))

	nonAdmin := *base
	nonAdmin.IsAdmin = false
	require.False(t, IsSupportSession(WithValidatedSupportSession(t.Context(), &nonAdmin)))

}
