package remotesessions

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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
		{"populated", []string{oauthwire.GrantTypeJWTBearer}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			view := preparationAPIView(&PreparationResult{GrantTypes: tc.grants})
			require.Equal(t, tc.grants, view.GrantTypes)
		})
	}
}

func TestPreparationResultRequiresMatchedClientGrantEvidence(t *testing.T) {
	t.Parallel()
	for _, evidence := range []struct {
		state, source string
	}{
		{PreparationStateReady, PreparationGrantSourceProviderReturned},
		{PreparationStateReady, PreparationGrantSourceAdministratorDeclared},
		{PreparationStatePublishedAcceptanceUnverified, PreparationGrantSourceCIMDPublished},
	} {
		state, source := evidence.state, evidence.source
		for _, tc := range []struct {
			name   string
			grants []string
			want   string
		}{
			{"unknown", nil, PreparationStateUnknownGrants},
			{"empty", []string{}, PreparationStateManualSetupRequired},
			{"interactive only", []string{"authorization_code"}, PreparationStateManualSetupRequired},
			{"jwt bearer", []string{oauthwire.GrantTypeJWTBearer}, state},
		} {
			t.Run(state+"/"+source+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				id := uuid.New()
				b := repo.RemoteSessionEmaBinding{RemoteSessionClientID: conv.ToNullUUID(id), GrantSource: conv.ToPGText(source)}
				client := repo.RemoteSessionClient{ID: id, GrantTypes: tc.grants}
				result := preparationResult(b, repo.RemoteSessionIssuer{}, client, state)
				require.Equal(t, tc.want, result.State)
				require.Equal(t, tc.grants, result.GrantTypes)
				require.Equal(t, source, result.GrantSource)
				b.GrantSource = conv.ToPGText(PreparationGrantSourceUnknown)
				require.Equal(t, PreparationStateUnknownGrants, preparationResult(b, repo.RemoteSessionIssuer{}, client, state).State)
			})
		}
	}
}
