package gcpauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvePrincipal_RejectsWIF(t *testing.T) {
	t.Parallel()

	_, err := NewResolver().ResolvePrincipal(t.Context(), Credential{
		WifPoolID:        "pool",
		WifProviderID:    "provider",
		WifProjectNumber: "123456789",
	})
	require.ErrorIs(t, err, ErrUnsupportedMode)
}

func TestServiceAccountEmail(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"sa@proj.iam.gserviceaccount.com",
		serviceAccountEmail([]byte(`{"type":"service_account","client_email":"sa@proj.iam.gserviceaccount.com"}`)),
	)
	require.Empty(t, serviceAccountEmail([]byte(`{"type":"authorized_user"}`)), "user ADC carries no service-account email")
	require.Empty(t, serviceAccountEmail(nil))
	require.Empty(t, serviceAccountEmail([]byte(`not json`)))
}
