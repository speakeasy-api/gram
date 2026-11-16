package mcp_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The runtime holds an agent to its recorded choices, so refresh does too: an
// agent whose chosen client was detached from the server is sent to reconnect
// rather than handed a token the runtime would refuse.
func TestHandleToken_AgentRefreshRefusesAnUnusableChoice(t *testing.T) {
	t.Parallel()

	const upstream = "https://agent-refresh-choice.example.com/mcp"
	ctx, ti := newTestMCPService(t)
	ti.service.manual = true
	authCtx := requireProjectAuthContext(t, ctx)
	_, issuer, sessionClient := seedPrivateToolsetWithIssuer(t, ctx, ti)
	chosen := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
	slug := "agent-refresh-choice-" + uuid.NewString()[:8]
	serverID := attachConsentRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID, issuer.ID, slug, upstream)
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     conv.ToNullUUID(serverID),
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            slug,
	})
	require.NoError(t, err)
	associations := upstreamclientsrepo.New(ti.conn)
	_, err = associations.AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		RemoteSessionClientID: chosen.ID,
		McpServerID:           serverID,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)

	agent, err := agentsrepo.New(ti.conn).CreateAgentWithID(ctx, agentsrepo.CreateAgentWithIDParams{
		ID:             uuid.New(),
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Refresh choice agent",
	})
	require.NoError(t, err)
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, serverID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), serverID)
	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   serverID.String(),
			authz.SelectorKeyProjectID:    authCtx.ProjectID.String(),
		},
	}})
	require.NoError(t, err)
	delegatedGrants, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	refreshToken := "agent-refresh-choice-" + uuid.NewString()
	refreshHash := sha256.Sum256([]byte(refreshToken))
	sessions := usersessions_repo.New(ti.conn)
	session, err := sessions.CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID:    issuer.ID,
		UserSessionClientID:    uuid.NullUUID{UUID: sessionClient.ID, Valid: true},
		SubjectUrn:             urn.NewAgentSubject(agent.ID),
		AuthorizerUserID:       pgtype.Text{String: authCtx.UserID, Valid: true},
		DelegatedGrants:        delegatedGrants,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		Jti:                    "agent-refresh-choice-jti-" + uuid.NewString(),
		RefreshTokenHash:       conv.ToPGText(base64.RawURLEncoding.EncodeToString(refreshHash[:])),
		RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true},
		SignInID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	_, err = sessions.RecordUpstreamClientSelections(ctx, usersessions_repo.RecordUpstreamClientSelectionsParams{
		UserSessionIssuerID: issuer.ID,
		CredentialKind:      upstreamclients.CredentialKindUserSession,
		CredentialID:        session.ID,
		Source:              upstreamclients.SelectionSourceIssuance,
		PickedMcpServerIds:  []uuid.UUID{},
		PickedClientIds:     []uuid.UUID{},
	})
	require.NoError(t, err)

	_, err = associations.DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
		McpServerID:           serverID,
		RemoteSessionClientID: chosen.ID,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)
	replacement := attachTestRemoteSessionClient(t, ctx, ti, authCtx, issuer.ID)
	_, err = associations.AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		RemoteSessionClientID: replacement.ID,
		McpServerID:           serverID,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)

	res := performRefreshRequest(ctx, ti, slug, sessionClient.ClientID, refreshToken)
	require.NoError(t, res.err)
	require.Equal(t, http.StatusBadRequest, res.code, res.body)
	require.Contains(t, res.body, reconnectDescription)
}
