package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type pendingSignInCall struct {
	userID, organizationID string
	projectID, mcpServerID uuid.UUID
}

type recordingPendingSignIn struct {
	calls []pendingSignInCall
	err   error
}

func (r *recordingPendingSignIn) ConnectPendingSignIn(_ context.Context, userID, organizationID string, projectID, mcpServerID uuid.UUID) error {
	r.calls = append(r.calls, pendingSignInCall{userID: userID, organizationID: organizationID, projectID: projectID, mcpServerID: mcpServerID})
	return r.err
}

// Only a first-party connect opened from a server card, signed in as a user,
// connects the server's sign-in provider before the consent page renders.
func TestConnectPendingSignInRunsOnlyForACardConnect(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	endpoint := &ResolvedMcpEndpoint{
		McpServerID:    uuid.NullUUID{UUID: uuid.New(), Valid: true},
		OrganizationID: "org_test",
		ProjectID:      uuid.New(),
	}
	user := urn.NewUserSubject("user_test")
	anonymous := urn.NewAnonymousSubject("session")
	cardConnect := AuthnChallengeState{FirstParty: true, ConnectPendingSignIn: true, Subject: &user}

	for name, tc := range map[string]struct {
		state    AuthnChallengeState
		endpoint *ResolvedMcpEndpoint
		calls    int
	}{
		"card connect":         {state: cardConnect, endpoint: endpoint, calls: 1},
		"plain first-party":    {state: AuthnChallengeState{FirstParty: true, Subject: &user}, endpoint: endpoint, calls: 0},
		"mcp client authorize": {state: AuthnChallengeState{ConnectPendingSignIn: true, Subject: &user}, endpoint: endpoint, calls: 0},
		"not a user":           {state: AuthnChallengeState{FirstParty: true, ConnectPendingSignIn: true, Subject: &anonymous}, endpoint: endpoint, calls: 0},
		"no mcp server":        {state: cardConnect, endpoint: &ResolvedMcpEndpoint{OrganizationID: "org_test"}, calls: 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			connector := &recordingPendingSignIn{}
			service := &Service{pendingSignIn: connector}
			require.NoError(t, service.connectPendingSignIn(t.Context(), logger, tc.endpoint, tc.state))
			require.Len(t, connector.calls, tc.calls)
			if tc.calls == 1 {
				require.Equal(t, pendingSignInCall{userID: "user_test", organizationID: "org_test", projectID: endpoint.ProjectID, mcpServerID: endpoint.McpServerID.UUID}, connector.calls[0])
			}
		})
	}

	t.Run("no connector", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, (&Service{}).connectPendingSignIn(t.Context(), logger, endpoint, cardConnect))
	})
}

// A failed connection stops the consent page with a message, rather than
// rendering a page that says there is nothing to connect.
func TestConnectPendingSignInFailureStopsTheConsentPage(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	user := urn.NewUserSubject("user_test")
	service := &Service{pendingSignIn: &recordingPendingSignIn{err: errors.New("provider refused registration")}}
	err := service.connectPendingSignIn(t.Context(), logger, &ResolvedMcpEndpoint{
		McpServerID:    uuid.NullUUID{UUID: uuid.New(), Valid: true},
		OrganizationID: "org_test",
		ProjectID:      uuid.New(),
	}, AuthnChallengeState{FirstParty: true, ConnectPendingSignIn: true, Subject: &user})

	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
}
