package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// agentGatewayFixture is one agent, one key, and two private member servers in
// the caller's project: the agent is granted one of them and not the other.
type agentGatewayFixture struct {
	agent   agentsrepo.Agent
	token   string
	granted string
	denied  string
	open    string
}

// mcpServerBySlug resolves the id a proxy-backed member's mcp:connect grant is
// keyed on. Grants for toolset-backed servers key on the toolset instead.
func mcpServerBySlug(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{
		Slug:      conv.ToPGText(slug),
		ProjectID: projectID,
	})
	require.NoError(t, err)
	return server.ID
}

func seedAgentGateway(t *testing.T, ctx context.Context, ti *testInstance) agentGatewayFixture {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ti.features.SetFlag(feature.FlagAgentManagement, authCtx.ActiveOrganizationID, true)
	ti.features.SetFlag(feature.FlagAgentIdentityCredentials, authCtx.ActiveOrganizationID, true)

	// Stored membership exists but must not matter: an agent gateway derives
	// its members from grants, so the meta server below is a decoy.
	metaSlug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, metaSlug, uuid.Nil)
	grantedSlug := "granted-" + uuid.NewString()[:8]
	deniedSlug := "denied-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "granted member", grantedSlug, 0, mcpservers.VisibilityPrivate)
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "denied member", deniedSlug, 1, mcpservers.VisibilityPrivate)
	publicSlug := "openmember-" + uuid.NewString()[:8]
	seedMetaMember(t, ctx, ti.conn, *authCtx.ProjectID, meta.ID, "public member", publicSlug, 2, mcpservers.VisibilityPublic)

	grantedServer := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, grantedSlug)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
		Name:           "Gateway agent " + uuid.NewString()[:8],
	})
	require.NoError(t, err)

	// Runtime admission intersects the credential's policy with the agent's own
	// and with its owner's, so a grant the owner lacks admits nothing. Seed
	// both, as the consent fixtures do.
	seedUserMCPConnectGrant(t, ctx, ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedServer.String())
	agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())
	seedPrincipalMCPConnectGrant(t, ctx, ti, authCtx.ActiveOrganizationID, agentPrincipal, grantedServer)

	// Credential policy: the immutable ceiling on this one key.
	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   grantedServer.String(),
			authz.SelectorKeyProjectID:    authCtx.ProjectID.String(),
		},
	}})
	require.NoError(t, err)
	delegated, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)

	token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		CreatedByUserID:        authCtx.UserID,
		Name:                   "gateway key " + uuid.NewString()[:8],
		KeyPrefix:              prefix,
		KeyHash:                hash,
		SubjectUrn:             pgtype.Text{String: urn.NewAgentSubject(agent.ID).String(), Valid: true},
		DelegatedGrants:        delegated,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	return agentGatewayFixture{agent: agent, token: token, granted: grantedSlug, denied: deniedSlug, open: publicSlug}
}

// serveAgentGatewayHTTP drives the handler the way the router does, including
// the {agentID} path parameter. The error is returned rather than swallowed:
// a rejected request never writes to the recorder, so its status would
// otherwise read as the recorder's default 200.
func serveAgentGatewayHTTP(t *testing.T, ti *testInstance, agentID, token string, body []byte) (*httptest.ResponseRecorder, error) {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, "/agent-mcp/"+agentID, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("agentID", agentID)
	r = r.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	err := ti.service.ServeAgentGateway(w, r)
	if err != nil {
		return w, fmt.Errorf("serve agent gateway: %w", err)
	}
	return w, nil
}

func requireAgentGatewayCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

// The whole point of the surface: membership follows the agent's grants, so a
// server it cannot connect to never appears, even though both are stored
// members of a gateway in the same project.
func TestServeAgentGateway_ListsOnlyGrantedServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{
		"name":      "list_servers",
		"arguments": map[string]any{},
	}))

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), fx.granted)
	require.NotContains(t, w.Body.String(), fx.denied)
	// A public server is anonymously reachable by anyone, so withholding it
	// here would hide something the agent can already reach without Gram.
	// Listing it is deliberate, not an admission leak.
	require.Contains(t, w.Body.String(), fx.open)
}

// An agent gateway answers initialize without an OAuth issuer gate: the agent
// key is the credential, so there is no consent page to send anyone to.
func TestServeAgentGateway_Initialize(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)

	w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeInitializeBody())

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.NotEmpty(t, envelope.Result)
}

func TestServeAgentGateway_RejectsMissingAndForeignCredentials(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	fx := seedAgentGateway(t, ctx, ti)
	other := seedAgentGateway(t, ctx, ti)

	body := makeInitializeBody()

	// No credential at all.
	_, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), "", body)
	requireAgentGatewayCode(t, err, oops.CodeUnauthorized)

	// A live key, but for a different agent: the address is wrong, so this must
	// not read as a denial that confirms the gateway exists.
	_, err = serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), other.token, body)
	requireAgentGatewayCode(t, err, oops.CodeNotFound)

	// A malformed agent id never reaches authentication.
	_, err = serveAgentGatewayHTTP(t, ti, "not-a-uuid", fx.token, body)
	requireAgentGatewayCode(t, err, oops.CodeNotFound)
}
