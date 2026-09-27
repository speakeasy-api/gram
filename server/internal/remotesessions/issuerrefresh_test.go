package remotesessions

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestVetRefreshedDocumentRequiresExactIssuer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, requested, served string
		wantErr                 bool
	}{
		{"exact", "https://issuer.example/tenant", "https://issuer.example/tenant", false},
		{"exact slash", "https://issuer.example/tenant/", "https://issuer.example/tenant/", false},
		{"added slash", "https://issuer.example/tenant", "https://issuer.example/tenant/", true},
		{"removed slash", "https://issuer.example/tenant/", "https://issuer.example/tenant", true},
		{"host case", "https://issuer.example/tenant", "https://ISSUER.example/tenant", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc := rfc8414Document{
				Issuer:                tc.served,
				AuthorizationEndpoint: "https://issuer.example/authorize",
				TokenEndpoint:         "https://issuer.example/token",
			}
			err := vetRefreshedDocument(doc, repo.RemoteSessionIssuer{Issuer: tc.requested})
			if tc.wantErr {
				var untrusted *untrustedDocumentError
				require.ErrorAs(t, err, &untrusted)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestBuildIssuerDraftCapturesAuthorizationGrantProfiles(t *testing.T) {
	t.Parallel()

	for _, profiles := range [][]string{nil, {}, {"urn:ietf:params:oauth:grant-profile:id-jag"}} {
		draft := buildIssuerDraft(rfc8414Document{AuthorizationGrantProfilesSupported: profiles}, "https://issuer.example", nil)
		require.NotNil(t, draft.AuthorizationGrantProfilesSupported)
		require.Equal(t, orEmptySlice(profiles), draft.AuthorizationGrantProfilesSupported)
	}
}
