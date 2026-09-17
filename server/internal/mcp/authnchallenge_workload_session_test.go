package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const workloadSessionSubject = "repo:acme/payments-api:ref:refs/heads/main"

// seedWorkloadIssuer registers an issuer the workload principal can name. Raw
// SQL because writes belong to the management API milestone.
func seedWorkloadIssuer(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := ti.conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `
		INSERT INTO workload_issuers (organization_id, name, issuer, jwks_uri)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, organizationID, "gh-actions-"+uuid.NewString()[:8], "https://token.actions.githubusercontent.com", "https://token.actions.githubusercontent.com/.well-known/jwks").Scan(&id)
	require.NoError(t, err)

	return id
}

func assignAgentToWorkload(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, issuerID uuid.UUID, subject string, agentID uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := ti.conn.QueryRow( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `
		INSERT INTO workload_agent_assignments (organization_id, workload_issuer_id, subject, agent_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, organizationID, issuerID, subject, agentID).Scan(&id)
	require.NoError(t, err)

	return id
}

func unassignWorkloadAgent(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) {
	t.Helper()

	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `UPDATE workload_agent_assignments SET deleted_at = clock_timestamp() WHERE id = $1`, id)
	require.NoError(t, err)
}

// seedWorkloadSession mints a session whose subject is a workload principal,
// carrying the endpoint-scoped ceiling and no authorizer.
func seedWorkloadSession(t *testing.T, ctx context.Context, ti *testInstance, fx agentConsentFixture, subject urn.SessionSubject) usersessionsrepo.UserSession {
	t.Helper()

	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{
		PrincipalUrn: "",
		Scope:        authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   fx.target.MCPResourceID.String(),
			authz.SelectorKeyProjectID:    fx.target.ProjectID.String(),
		},
	}})
	require.NoError(t, err)
	delegatedGrants, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)

	refreshHash := sha256.Sum256([]byte("workload-refresh-" + uuid.NewString()))
	jtiHash := sha256.Sum256([]byte("workload-jti-" + uuid.NewString()))
	session, err := usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
		UserSessionClientID: uuid.NullUUID{UUID: fx.client.ID, Valid: true},
		SubjectUrn:          subject,
		// No authorizer: a workload records no approving human.
		AuthorizerUserID:       pgtype.Text{String: "", Valid: false},
		DelegatedGrants:        delegatedGrants,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		Jti:                    base64.RawURLEncoding.EncodeToString(jtiHash[:]),
		RefreshTokenHash:       base64.RawURLEncoding.EncodeToString(refreshHash[:]),
		RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
	})
	require.NoError(t, err)

	return session
}

func workloadSessionEndpoint(fx agentConsentFixture) *mcp.ResolvedMcpEndpoint {
	return &mcp.ResolvedMcpEndpoint{
		AudienceURN:         urn.NewToolset(fx.toolset.ID).String(),
		OrganizationID:      fx.orgID,
		ProjectID:           fx.target.ProjectID,
		RouteBase:           "mcp",
		Slug:                fx.toolset.McpSlug.String,
		ToolsetID:           uuid.NullUUID{UUID: fx.toolset.ID, Valid: true},
		UserSessionIssuerID: fx.target.UserSessionIssuerID,
	}
}

func mintWorkloadBearer(t *testing.T, ti *testInstance, fx agentConsentFixture, session usersessionsrepo.UserSession) string {
	t.Helper()
	return mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)
}

func mintSessionBearerExpiringAt(t *testing.T, ti *testInstance, fx agentConsentFixture, session usersessionsrepo.UserSession, expiresAt time.Time) string {
	t.Helper()

	token, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:   session.SubjectUrn,
		Audience:  urn.NewToolset(fx.toolset.ID).String(),
		Issuer:    ti.serverURL.JoinPath("mcp", fx.toolset.McpSlug.String).String(),
		ExpiresAt: &expiresAt,
		ClientID:  fx.client.ClientID,
		JTI:       session.Jti,
	})
	require.NoError(t, err)

	return token
}

// A workload session acts as its own principal, with the authority of the agent
// assigned to it. The actor is the workload, never the agent and never a user:
// resolving it as a user would seed user:all and hand the machine every grant
// written for every member.
func TestApplyIssuerGate_WorkloadSessionActsThroughItsAssignedAgent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)

	w := httptest.NewRecorder()
	admittedCtx, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, mintWorkloadBearer(t, ti, fx, session), ti.serverURL.String(), endpoint)
	require.NoError(t, err)

	actor, ok := contextvalues.AuthenticatedActor(admittedCtx)
	require.True(t, ok)
	require.Equal(t, urn.NewWorkloadPrincipal(issuerID, workloadSessionSubject).String(), actor.String())
	require.Equal(t, urn.PrincipalTypeWorkload, actor.Type)

	authCtx, ok := contextvalues.GetAuthContext(admittedCtx)
	require.True(t, ok)
	require.Empty(t, authCtx.UserID, "a workload session names no acting user")
	require.Empty(t, authCtx.APIKeyID)

	authorizer, owner, ok := contextvalues.PrincipalCredentialProvenance(admittedCtx)
	require.True(t, ok)
	require.Empty(t, authorizer, "a workload session records no approving human")
	require.Equal(t, fx.userID, owner)
}

// The assignment is what confers authority, so removing it takes the authority
// away on the next request.
func TestApplyIssuerGate_WorkloadSessionWithNoAssignedAgentIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignment := assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)
	token := mintWorkloadBearer(t, ti, fx, session)

	w := httptest.NewRecorder()
	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "the assigned workload must be admitted, or the refusal below proves nothing")

	unassignWorkloadAgent(t, ctx, ti, assignment)

	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func softDeleteWorkloadIssuer(t *testing.T, ctx context.Context, ti *testInstance, id uuid.UUID) {
	t.Helper()

	_, err := ti.conn.Exec( //nolint:glint // notestingrawsql: no create query exists yet; writes belong to the management API milestone
		ctx, `UPDATE workload_issuers SET deleted_at = clock_timestamp() WHERE id = $1`, id)
	require.NoError(t, err)
}

// Deleting the issuer that vouched for a workload withdraws the authority of
// sessions minted before the delete, even though the assignment stays live.
func TestApplyIssuerGate_WorkloadSessionRefusedWhenItsIssuerIsDeleted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)
	token := mintWorkloadBearer(t, ti, fx, session)

	w := httptest.NewRecorder()
	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "the workload must be admitted before the delete, or the refusal below proves nothing")

	softDeleteWorkloadIssuer(t, ctx, ti, issuerID)

	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

// The agent authorization rollout gates workload sessions exactly as it gates
// agent sessions. The gate answers a refused bearer with a re-auth challenge
// either way, so the refusal surfaces as unauthorized.
func TestApplyIssuerGate_WorkloadSessionHiddenWhenAgentRolloutDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)
	token := mintWorkloadBearer(t, ti, fx, session)

	w := httptest.NewRecorder()
	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "the workload must be admitted while the rollout is on, or the refusal below proves nothing")

	ti.features.SetFlag(feature.FlagAgentMCPAuthorizationM2, fx.orgID, false)

	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

// An agent's lifecycle reaches every workload assigned to it, which is the
// revocation path an operator gets for free by suspending the agent.
func TestApplyIssuerGate_WorkloadSessionRefusedWhenItsAgentIsSuspended(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)
	token := mintWorkloadBearer(t, ti, fx, session)

	w := httptest.NewRecorder()
	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "the workload must be admitted before the suspension, or the refusal below proves nothing")

	_, err = agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)

	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

// A workload inherits only its own agent's policy. An agent assigned to nothing
// grants it nothing, even when that agent may reach this endpoint itself.
func TestApplyIssuerGate_WorkloadSessionDoesNotInheritAnUnassignedAgentsPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newAgentConsentFixture(t, ctx, ti)
	assigned := createConsentAgent(t, ctx, ti, fx, "Assigned agent")
	unrelated := createConsentAgent(t, ctx, ti, fx, "Unrelated agent")
	// Only the unrelated agent may connect; the assigned one holds nothing.
	// Every member may connect too, which a workload must never inherit.
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, unrelated.ID.String()), fx.target.MCPResourceID)
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, urn.AllUsersPrincipalID), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignment := assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, assigned.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)
	token := mintWorkloadBearer(t, ti, fx, session)

	w := httptest.NewRecorder()
	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	unassignWorkloadAgent(t, ctx, ti, assignment)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, unrelated.ID)

	w = httptest.NewRecorder()
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "the same workload must be admitted through an agent that holds the grant, or the refusal above proves nothing")
}
