package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestConsentDirectCredentialDependencyFailureIsNotAChangedCredential(t *testing.T) {
	t.Parallel()
	pool, err := pgxpool.New(t.Context(), "postgres://localhost/unused")
	require.NoError(t, err)
	pool.Close()
	service := &Service{db: pool, logger: testenv.NewLogger(t), authEnforcement: authenforcement.NewGate(pool)}
	endpoint := &ResolvedMcpEndpoint{OrganizationID: "organization", ProjectID: uuid.New(), McpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	subject := urn.NewUserSubject("human")

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	serverRow := &mcpservers_repo.McpServer{RemoteMcpServerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}
	err = service.serveConsentProxiedMCP(httptest.NewRecorder(), req, endpoint, AuthnChallengeState{Subject: &subject}, consentToolInventory{}, serverRow, "")
	var failure *oops.ShareableError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, oops.CodeUnavailable, failure.Code)

	err = service.validateDirectConsentInventory(t.Context(), endpoint, &consentToolInventory{}, nil)
	require.Error(t, err)
	require.NotErrorIs(t, err, errConsentConfigurationChanged)
}

func TestBindDirectConsentInventory(t *testing.T) {
	t.Parallel()
	first, second := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name      string
		before    *uuid.UUID
		after     *authClientResolution
		complete  bool
		wantError bool
	}{
		{name: "new typed attempt", after: &authClientResolution{clientID: first}},
		{name: "same typed choice", before: &first, after: &authClientResolution{clientID: first}},
		{name: "different typed choice", before: &first, after: &authClientResolution{clientID: second}, wantError: true},
		{name: "cannot downgrade typed attempt", before: &first, wantError: true},
		{name: "cannot relabel legacy capture", complete: true, after: &authClientResolution{clientID: first}, wantError: true},
		{name: "legacy capture unchanged", complete: true},
		{name: "no auth is explicitly bound", after: &authClientResolution{outcome: authClientNoAuth}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			draft := consentToolInventory{DirectAuthClient: tc.before, Complete: tc.complete}
			err := bindDirectConsentInventory(&draft, tc.after)
			if tc.wantError {
				require.ErrorIs(t, err, errConsentConfigurationChanged)
				return
			}
			require.NoError(t, err)
			if tc.after == nil {
				require.Nil(t, draft.DirectAuthClient)
			} else {
				require.NotNil(t, draft.DirectAuthClient)
				require.Equal(t, tc.after.clientID, *draft.DirectAuthClient)
			}
		})
	}
}
