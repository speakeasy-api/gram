package mcp_test

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"goa.design/goa/v3/security"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/spendrules"
	spendcelenv "github.com/speakeasy-api/gram/server/internal/spendrules/celenv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Claude Tag delivers hooks and OTEL through Anthropic's Agent Proxy, which
// exchanges the channel's identity token at the organization's device-agent
// token endpoint and attaches the result as `Authorization: Bearer` to every
// request on the allowed hooks paths. These tests drive that path end to end:
// Anthropic's subject through the guided-setup access rule, the exchange, then
// hooks ingestion with no API key.

// newClaudeTagDeviceAgentFixture is the device-agent fixture with the access rule
// the guided setup writes for Claude Tag on the same trusted issuer, assigned
// to the fixture's agent. f.subject is one channel of the admitted Anthropic
// organization.
func newClaudeTagDeviceAgentFixture(t *testing.T) deviceAgentGrantFixture {
	t.Helper()

	f := newDeviceAgentGrantFixture(t)
	rule := catalogRule(t, "claude-tag", map[string]string{"org_id": catalogTestOrganization})
	fixtures := testrepo.New(f.ti.conn)
	require.NoError(t, fixtures.CreateWorkloadIdentityRuleFixture(f.ctx, testrepo.CreateWorkloadIdentityRuleFixtureParams{
		OrganizationID:   f.fx.orgID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: f.issuerID,
		Subject:          rule,
		MatchKind:        "wildcard",
	}))
	require.NoError(t, fixtures.CreateWorkloadAgentAssignmentFixture(f.ctx, testrepo.CreateWorkloadAgentAssignmentFixtureParams{
		OrganizationID:   f.fx.orgID,
		WorkloadIssuerID: f.issuerID,
		Subject:          rule,
		MatchKind:        "wildcard",
		AgentID:          f.agentID,
	}))
	f.subject = strings.TrimSuffix(rule, "*") + "agent-channel-1"
	return f
}

// claudeTagDeviceAgentToken exchanges an assertion for f.subject the way Agent
// Proxy does when the token endpoint is registered with no Issuer URL: the
// assertion's audience is the token endpoint itself.
func claudeTagDeviceAgentToken(t *testing.T, f deviceAgentGrantFixture) string {
	t.Helper()

	w := f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL))
	requireDeviceAgentToken(t, w, f.fx.orgID, f.issuerID, f.subject)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	token, ok := resp["access_token"].(string)
	require.True(t, ok)
	return token
}

// sessionCaptureFeatures enables session capture so ingested prompts persist a
// chat row, and leaves every other product feature off.
type sessionCaptureFeatures struct{}

func (sessionCaptureFeatures) IsFeatureEnabled(_ context.Context, _ string, feature productfeatures.Feature) (bool, error) {
	return feature == productfeatures.FeatureSessionCapture, nil
}

// newClaudeTagHooksService builds the hooks service on f's database, authz
// engine and device-agent token signer, so a token f's exchange issued is
// admitted there exactly as in production. The agent is granted
// org:hooks_ingest across the organization, and so is its owner: workload
// admission bounds every request by the owner's live permissions.
func newClaudeTagHooksService(t *testing.T, f deviceAgentGrantFixture) *hooks.Service {
	t.Helper()

	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	seedPrincipalGrant(t, f.ctx, f.ti, f.fx.orgID, agent, authz.ScopeOrgHooksIngest, f.fx.orgID)
	seedPrincipalGrant(t, f.ctx, f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, f.fx.userID), authz.ScopeOrgHooksIngest, f.fx.orgID)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	spendEngine, err := spendcelenv.New()
	require.NoError(t, err)
	spendGate, err := spendrules.NewGate(f.ti.logger, f.ti.cacheAdapter, spendEngine)
	require.NoError(t, err)
	chatWriter, shutdownWriter := chat.NewChatMessageWriter(f.ti.logger, f.ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { require.NoError(t, shutdownWriter(context.Background())) })
	callerIssuer, err := sharedCallerIssuer()
	require.NoError(t, err)

	return hooks.NewService(
		f.ti.logger,
		f.ti.conn,
		f.ti.tracerProvider,
		testenv.NewMeterProvider(t),
		nil,
		gcp.NewNoopPublisher[*otelv1.InboundLogRecord](),
		f.ti.sessionManager,
		f.ti.cacheAdapter,
		ratelimit.NewRedisStore(redisClient),
		nil,
		nil,
		f.ti.authzEngine,
		f.ti.audit,
		sessionCaptureFeatures{},
		nil,
		nil,
		nil,
		risk.NewPolicyBypassEvaluator(f.ti.logger, f.ti.conn),
		spendGate,
		shadowmcp.NewClient(f.ti.logger, f.ti.conn, f.ti.cacheAdapter, f.ti.serverURL),
		chatWriter,
		nil,
		nil,
		nil,
		f.ti.serverURL,
		orghost.New(orghost.Config{ServerURL: f.ti.serverURL, SiteURL: f.ti.siteURL, PlatformHosts: nil, LegacyDefaultHost: nil, NewOrganizationDefaultHost: nil}),
		"test-jwt-secret",
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
		deviceagenttoken.New(callerIssuer, f.ti.conn),
	)
}

// projectSlug is the slug of the fixture's MCP server project, the project
// Claude Tag's plugin names on every hooks request.
func projectSlug(t *testing.T, f deviceAgentGrantFixture) string {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)
	return *authCtx.ProjectSlug
}

// claudeTagPrompt is the canonical prompt the Speakeasy plugin's relay sends
// for a Claude Tag turn, authenticated only by the bearer Agent Proxy attached.
func claudeTagPrompt(token, project, sessionID string) *gen.IngestPayload {
	prompt := "summarize this channel"
	authorization := "Bearer " + token
	return &gen.IngestPayload{
		SchemaVersion:    "hook.ingest.v1",
		Source:           &gen.HookIngestSource{Adapter: "claude"},
		Session:          &gen.HookIngestSession{ID: &sessionID},
		Event:            &gen.HookIngestEvent{Type: "prompt.submitted"},
		Data:             &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &prompt}},
		DeviceAgentToken: &authorization,
		ProjectSlugInput: &project,
	}
}

// requireAgentChat asserts the session's chat row names the fixture's agent.
// A workload's session ids are namespaced under its agent, so the row is the
// one the agent's id and the reported session id hash to.
func requireAgentChat(t *testing.T, f deviceAgentGrantFixture, sessionID string) {
	t.Helper()

	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String())
	stored, err := chatrepo.New(f.ti.conn).GetChat(t.Context(), chatrepo.GetChatParams{
		ID:        chat.SessionIDToChatID(agent.String() + ":" + sessionID),
		ProjectID: f.fx.target.ProjectID,
	})
	require.NoError(t, err)
	require.True(t, stored.AgentID.Valid, "the Claude Tag session records the agent it acts for")
	require.Equal(t, f.agentID, stored.AgentID.UUID)
	require.False(t, stored.UserID.Valid, "no human is attributed")
}

// The access rule the guided setup writes for Claude Tag admits a channel at
// the organization's device-agent token endpoint, as it does at /mcp/<slug>.
func TestDeviceAgentToken_ClaudeTagChannelReceivesAToken(t *testing.T) {
	t.Parallel()

	f := newClaudeTagDeviceAgentFixture(t)
	claudeTagDeviceAgentToken(t, f)
}

// The organization ID in the subject is what pins the rule to one customer, so
// another Anthropic organization's channel is refused at the device-agent
// endpoint too.
func TestDeviceAgentToken_ClaudeTagRefusesAnotherOrganization(t *testing.T) {
	t.Parallel()

	f := newClaudeTagDeviceAgentFixture(t)
	f.subject = "wimse://identity.anthropic.com/org/org-456/agent/agent-channel-1"
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, f.tokenURL)))
}

// An assertion Agent Proxy addressed to the MCP server's authorization server
// cannot be exchanged for a hooks token: each connection's audience is its own.
func TestDeviceAgentToken_ClaudeTagMCPAudienceRefused(t *testing.T) {
	t.Parallel()

	f := newClaudeTagDeviceAgentFixture(t)
	requireWorkloadGrantRefused(t, f.exchangeDeviceAgent(t, f.assertion(t, f.advertisedIssuer)))
}

// A Claude Tag channel's hooks arrive with no API key: the token the exchange
// issued authenticates them, and the session is recorded as the agent's.
func TestClaudeTagWorkload_HooksWithoutAnAPIKeyAreAttributedToTheAgent(t *testing.T) {
	t.Parallel()

	f := newClaudeTagDeviceAgentFixture(t)
	token := claudeTagDeviceAgentToken(t, f)
	hooksService := newClaudeTagHooksService(t, f)

	sessionID := "claude-tag-" + uuid.NewString()
	result, err := hooksService.Ingest(t.Context(), claudeTagPrompt(token, projectSlug(t, f), sessionID))
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
	requireAgentChat(t, f, sessionID)
}

// A plugin that still carries the organization's shared hooks key while Agent
// Proxy attaches the workload's token is attributed to the agent: the token is
// read first and the key is never consulted.
func TestClaudeTagWorkload_TokenWinsOverTheSharedHooksKey(t *testing.T) {
	t.Parallel()

	f := newClaudeTagDeviceAgentFixture(t)
	token := claudeTagDeviceAgentToken(t, f)
	hooksService := newClaudeTagHooksService(t, f)

	sessionID := "claude-tag-" + uuid.NewString()
	payload := claudeTagPrompt(token, projectSlug(t, f), sessionID)
	payload.ApikeyToken = new("gram_key_not_a_real_key")
	_, err := hooksService.Ingest(t.Context(), payload)
	require.NoError(t, err)
	requireAgentChat(t, f, sessionID)
}

// Claude Code's OTEL exporter in a Claude Tag sandbox sends logs and metrics
// to the same allowed paths, so they carry the same token, and are admitted as
// the channel's workload acting for its agent.
func TestClaudeTagWorkload_OTELIsAdmittedAsTheAgent(t *testing.T) {
	t.Parallel()

	f := newClaudeTagDeviceAgentFixture(t)
	token := claudeTagDeviceAgentToken(t, f)
	hooksService := newClaudeTagHooksService(t, f)
	tokenScheme := &security.JWTScheme{Name: constants.DeviceAgentTokenSecurityScheme, Scopes: []string{}, RequiredScopes: []string{}}
	projectScheme := &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{}}

	for _, method := range []string{"logs", "metrics"} {
		ctx, err := hooksService.JWTAuth(context.WithValue(t.Context(), goa.MethodKey, method), token, tokenScheme)
		require.NoError(t, err, method)
		ctx, err = hooksService.APIKeyAuth(ctx, projectSlug(t, f), projectScheme)
		require.NoError(t, err, method)

		actor, ok := contextvalues.AuthenticatedActor(ctx)
		require.True(t, ok)
		require.Equal(t, urn.NewWorkloadPrincipal(f.issuerID, f.subject).String(), actor.String(), "the channel stays the authenticated actor")
		agent, ok := deviceagenttoken.EffectiveAgent(ctx)
		require.True(t, ok)
		require.Equal(t, f.agentID.String(), agent.ID, "telemetry is attributed to the channel's agent")
	}
}

// Agent Proxy drops a cached token on 401. A token that has expired is refused
// that way, so the next request triggers a fresh exchange.
func TestClaudeTagWorkload_ExpiredTokenIsUnauthorized(t *testing.T) {
	t.Parallel()

	f := newClaudeTagDeviceAgentFixture(t)
	claudeTagDeviceAgentToken(t, f)
	hooksService := newClaudeTagHooksService(t, f)
	callerIssuer, err := sharedCallerIssuer()
	require.NoError(t, err)
	expired, _, err := deviceagenttoken.New(callerIssuer, f.ti.conn).Mint(f.fx.orgID, urn.NewWorkloadPrincipal(f.issuerID, f.subject), time.Now().Add(-2*deviceagenttoken.Lifetime))
	require.NoError(t, err)

	_, err = hooksService.Ingest(t.Context(), claudeTagPrompt(expired, projectSlug(t, f), "claude-tag-"+uuid.NewString()))
	requireShareableCode(t, err, oops.CodeUnauthorized)
}

// The device-agent token endpoint, registered in Claude Tag with no Issuer
// URL, is the assertion audience as Claude stores it: the host lowercased and
// no trailing slash. TokenURL already has that form.
func TestDeviceAgentToken_TokenURLIsClaudeTagsStoredAudience(t *testing.T) {
	t.Parallel()

	f := newClaudeTagDeviceAgentFixture(t)
	parsed, err := url.Parse(f.tokenURL)
	require.NoError(t, err)
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, strings.ToLower(parsed.Host), parsed.Host)
	require.Empty(t, parsed.Port(), "Claude drops a port from the token endpoint")
	require.Empty(t, parsed.RawQuery)
	require.False(t, strings.HasSuffix(f.tokenURL, "/"))
}
