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

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const workloadSessionSubject = "repo:acme/payments-api:ref:refs/heads/main"

// newWorkloadConsentFixture gives the owner MCP access so tests of assignment,
// session ceilings, and agent policy do not fail at the independent owner gate.
func newWorkloadConsentFixture(t *testing.T, ctx context.Context, ti *testInstance) agentConsentFixture {
	t.Helper()
	fx := newAgentConsentFixture(t, ctx, ti)
	selector, err := authz.NewSelector(authz.ScopeMCPConnect, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: fx.orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID),
		Scope: string(authz.ScopeMCPConnect), Selectors: selector,
	})
	require.NoError(t, err)
	return fx
}

// seedWorkloadIssuer registers an organization-tier issuer the workload
// principal can name.
func seedWorkloadIssuer(t *testing.T, ctx context.Context, ti *testInstance, organizationID string) uuid.UUID {
	t.Helper()

	id, err := testrepo.New(ti.conn).CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "gh-actions-" + uuid.NewString()[:8],
		Issuer:         "https://token.actions.githubusercontent.com",
		JwksUri:        "https://token.actions.githubusercontent.com/.well-known/jwks",
	})
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
		RefreshTokenHash:       conv.ToPGText(base64.RawURLEncoding.EncodeToString(refreshHash[:])),
		RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
		SignInID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
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
	fx := newWorkloadConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)

	w := httptest.NewRecorder()
	admittedCtx, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time), ti.serverURL.String(), endpoint)
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

	identity, ok := mcpidentity.FromContext(admittedCtx)
	require.True(t, ok)
	require.Equal(t, mcpidentity.KindAgent, identity.Kind())
	require.Equal(t, agent.ID.String(), identity.AgentID())
	require.Equal(t, session.Jti, identity.SessionJTI())
	require.WithinDuration(t, session.ExpiresAt.Time, identity.ExpiresAt(), time.Second)

	// Exercise the actual hosted checkpoint, not just context getters. The
	// machine retains its workload actor but is controlled by its assigned agent.
	serverID, err := testrepo.New(ti.conn).CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{
		ID: uuid.New(), ProjectID: fx.target.ProjectID, ToolsetID: conv.ToNullUUID(fx.toolset.ID), Visibility: "private",
	})
	require.NoError(t, err)
	checkpoint, err := mcptoolexecution.NewHostedCheckpoint(ti.conn, nil, testenv.NewLogger(t), nil)
	require.NoError(t, err)
	source := mcptoolexecution.ServerSource{FrontingServerID: conv.ToNullUUID(serverID)}
	disposition, err := checkpoint.Evaluate(admittedCtx, fx.orgID, source)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionContinue, disposition.Kind())

	require.NoError(t, testrepo.New(ti.conn).InsertKillswitchPrescriptionFixture(ctx, testrepo.InsertKillswitchPrescriptionFixtureParams{
		PrescriptionID: uuid.New(), OrganizationID: fx.orgID,
		DefinitionKey: string(mcptoolexecution.DefinitionKeyMCPToolExecution),
		PrincipalKind: string(mcptoolexecution.PrincipalKindAgent), PrincipalKey: agent.ID.String(),
		ResourceKind: string(mcptoolexecution.ResourceKindMCPServer), ResourceScope: "all",
		InternalNote: "Machine regression", ExternalNote: "Agent paused.",
	}))
	disposition, err = checkpoint.Evaluate(admittedCtx, fx.orgID, source)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionMatchedDenial, disposition.Kind())

	_, err = agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: fx.orgID, ID: agent.ID})
	require.NoError(t, err)
	disposition, err = checkpoint.Evaluate(admittedCtx, fx.orgID, source)
	require.NoError(t, err)
	require.Equal(t, killswitches.TransportDispositionInfrastructureRejection, disposition.Kind(), "even an already admitted context must fail closed after suspension")
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time), ti.serverURL.String(), endpoint)
	var suspended *oops.ShareableError
	require.ErrorAs(t, err, &suspended)
	require.Equal(t, oops.CodeUnauthorized, suspended.Code)
}

func TestApplyIssuerGate_WorkloadSessionFollowsLiveOwnerPermissions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Owner bounded workload")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)
	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)
	session := seedWorkloadSession(t, ctx, ti, fx, urn.NewWorkloadSubject(issuerID, workloadSessionSubject))
	endpoint := workloadSessionEndpoint(fx)
	token := mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)
	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
	require.NoError(t, err)

	selector, err := authz.NewSelector(authz.ScopeMCPBlockedConnect, fx.target.MCPResourceID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: fx.orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, fx.userID),
		Scope: string(authz.ScopeMCPBlockedConnect), Selectors: selector,
	})
	require.NoError(t, err)
	_, _, _, err = ti.service.ApplyIssuerGate(t.Context(), httptest.NewRecorder(), token, ti.serverURL.String(), endpoint)
	var denied *oops.ShareableError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, oops.CodeUnauthorized, denied.Code, "a resource-bound session is withdrawn when the owner loses endpoint access")
}

// The assignment is what confers authority, so removing it takes the authority
// away on the next request.
func TestApplyIssuerGate_WorkloadSessionWithNoAssignedAgentIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignment := assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)
	token := mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)

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

func softDeleteWorkloadIssuer(t *testing.T, ctx context.Context, ti *testInstance, organizationID string, id uuid.UUID) {
	t.Helper()

	deleted, err := testrepo.New(ti.conn).SoftDeleteWorkloadIssuerFixture(ctx, testrepo.SoftDeleteWorkloadIssuerFixtureParams{
		ID: id, OrganizationID: organizationID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
}

// Deleting the issuer that vouched for a workload withdraws the authority of
// sessions minted before the delete, even though the assignment stays live.
func TestApplyIssuerGate_WorkloadSessionRefusedWhenItsIssuerIsDeleted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	fx := newWorkloadConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)
	token := mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)

	w := httptest.NewRecorder()
	_, _, _, err := ti.service.ApplyIssuerGate(t.Context(), w, token, ti.serverURL.String(), endpoint)
	require.NoError(t, err, "the workload must be admitted before the delete, or the refusal below proves nothing")

	softDeleteWorkloadIssuer(t, ctx, ti, fx.orgID, issuerID)

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
	fx := newWorkloadConsentFixture(t, ctx, ti)
	agent := createConsentAgent(t, ctx, ti, fx, "Workload deploy agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	issuerID := seedWorkloadIssuer(t, ctx, ti, fx.orgID)
	assignAgentToWorkload(t, ctx, ti, fx.orgID, issuerID, workloadSessionSubject, agent.ID)

	subject := urn.NewWorkloadSubject(issuerID, workloadSessionSubject)
	session := seedWorkloadSession(t, ctx, ti, fx, subject)
	endpoint := workloadSessionEndpoint(fx)
	token := mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)

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
	fx := newWorkloadConsentFixture(t, ctx, ti)
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
	token := mintSessionBearerExpiringAt(t, ti, fx, session, session.ExpiresAt.Time)

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
