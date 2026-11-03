package mcp_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/auth"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/stretchr/testify/require"
)

func TestAgentClientRegistrationConcurrentIssuerRetirement(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx, agent, _, session := seedAgentRefreshSession(t, ctx, ti)
	raw, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{OrganizationID: fx.orgID, CreatedByUserID: fx.userID, Name: "concurrent registration", KeyPrefix: prefix, KeyHash: hash, SubjectUrn: pgtype.Text{String: session.SubjectUrn.String(), Valid: true}, DelegatedGrants: session.DelegatedGrants, DelegatedGrantsVersion: session.DelegatedGrantsVersion, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	require.NoError(t, err)
	signer := newAssertionSigner(t)
	body, err := json.Marshal(map[string]any{"client_name": "concurrent agent", "agent_urn": urn.NewAgentSubject(agent.ID).String(), "grant_types": []string{"client_credentials"}, "token_endpoint_auth_method": "private_key_jwt", "jwks": json.RawMessage(signer.jwks)})
	require.NoError(t, err)
	request := newRegisterRequest(t, fx.toolset.McpSlug.String, string(body))
	request.Header.Set("Authorization", "Bearer "+raw)
	retirement := testenv.BeginTx(t, ctx, ti.conn)
	defer func() { _ = retirement.Rollback(ctx) }()
	_, err = usersessionsrepo.New(retirement).GetOrganizationManagedUserSessionIssuerByIDForUpdate(ctx, usersessionsrepo.GetOrganizationManagedUserSessionIssuerByIDForUpdateParams{ID: fx.target.UserSessionIssuerID, OrganizationID: fx.orgID})
	require.NoError(t, err)
	_, err = testrepo.New(retirement).SoftDeleteAttachmentConfigFixture(ctx, testrepo.SoftDeleteAttachmentConfigFixtureParams{ID: fx.target.UserSessionIssuerID, ProjectID: uuid.NullUUID{UUID: fx.target.ProjectID, Valid: true}})
	require.NoError(t, err)

	// Reads still see the live issuer; insertion must wait for its lock and
	// re-evaluate after retirement, rather than fail with a serialization error.
	result := make(chan error, 1)
	response := httptest.NewRecorder()
	go func() { result <- ti.service.HandleRegister(response, request) }()
	testenv.WaitForBackendsBlockedBy(t, ctx, ti.conn, testenv.BackendPID(retirement), 1)
	require.NoError(t, retirement.Commit(ctx))
	select {
	case err := <-result:
		var shared *oops.ShareableError
		require.ErrorAs(t, err, &shared)
		require.Equal(t, oops.CodeNotFound, shared.Code)
		require.Empty(t, response.Body.String())
	case <-time.After(5 * time.Second):
		t.Fatal("registration did not finish after issuer retirement")
	}
}
