package mcp_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
)

// catalogTestOrganization stands in for a customer's Anthropic organization ID.
const catalogTestOrganization = "org-123"

// catalogRule fills a catalog platform's subject template the way the guided
// setup does, and returns the access rule it writes.
func catalogRule(t *testing.T, key string, values map[string]string) string {
	t.Helper()

	platforms, err := catalog.Embedded().Platforms(t.Context())
	require.NoError(t, err)
	for _, platform := range platforms {
		if platform.Key != key {
			continue
		}
		rule := platform.Subject.Template
		for name, value := range values {
			rule = strings.ReplaceAll(rule, "{"+name+"}", value)
		}
		require.NotContains(t, rule, "{", "every placeholder must be filled")
		require.True(t, platform.Subject.Wildcard)
		return rule + "*"
	}
	require.FailNow(t, "catalog has no platform "+key)
	return ""
}

// newCatalogWorkloadGrantFixture is newWorkloadGrantFixture with the access
// rule the guided setup writes for Claude Tag: a wildcard over one Anthropic
// organization's agents, assigned to one agent. Nobody can sign as Anthropic in
// a test, so a dev-idp stands in for its issuer; the subjects are Claude Tag's.
func newCatalogWorkloadGrantFixture(t *testing.T) workloadGrantFixture {
	t.Helper()

	issuer := devidptest.Launch(t, devidptest.LaunchOpts{EnableWorkOS: false, Key: nil, TLS: true})
	jwksURI := oauthtest.DiscoverWorkloadJWKSURI(t, issuer)

	ctx, ti := newTestMCPServiceWithTunnelPublicConfigAndCacheWrapper(t, testenv.NewLogger(t), testenv.NewMeterProvider(t), &mockIdentityResolver{hasAccessOK: true}, mcp.TunnelPublicConfig{
		SessionTTL:         0,
		LiveSessionCap:     0,
		InitializeRate:     ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		RequestRate:        ratelimit.Rate{Tokens: 0, Interval: 0, Burst: 0},
		MaxRequestLifetime: 0,
	}, nil, guardian.WithTLSRootCAs(issuer.RootCAs()))
	fx := newAgentConsentFixture(t, ctx, ti)

	agent := createConsentAgent(t, ctx, ti, fx, "Claude Tag agent")
	seedPrincipalMCPConnectGrant(t, ctx, ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), fx.target.MCPResourceID)

	rule := catalogRule(t, "claude-tag", map[string]string{"org_id": catalogTestOrganization})
	fixtures := testrepo.New(ti.conn)
	issuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: fx.orgID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Name:           "Claude Tag",
		Issuer:         issuer.OAuth21URL,
		JwksUri:        jwksURI,
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.CreateWorkloadIdentityRuleFixture(ctx, testrepo.CreateWorkloadIdentityRuleFixtureParams{
		OrganizationID:   fx.orgID,
		ProjectID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		WorkloadIssuerID: issuerID,
		Subject:          rule,
		MatchKind:        "wildcard",
	}))
	require.NoError(t, fixtures.CreateWorkloadAgentAssignmentFixture(ctx, testrepo.CreateWorkloadAgentAssignmentFixtureParams{
		OrganizationID:   fx.orgID,
		WorkloadIssuerID: issuerID,
		Subject:          rule,
		MatchKind:        "wildcard",
		AgentID:          agent.ID,
	}))

	slug := fx.toolset.McpSlug.String
	advertisedIssuer, _ := fetchAdvertisedIssuer(t, ctx, ti, slug)

	return workloadGrantFixture{
		ti:       ti,
		fx:       fx,
		issuer:   issuer,
		issuerID: issuerID,
		agentID:  agent.ID,
		// One Slack channel's agent in the admitted organization.
		subject:          strings.TrimSuffix(rule, "*") + "agent-channel-1",
		resource:         strings.TrimSuffix(ti.serverURL.String(), "/") + "/mcp/" + slug,
		advertisedIssuer: advertisedIssuer,
	}
}

// claudeTagAssertion signs an assertion for a Claude Tag subject.
func claudeTagAssertion(t *testing.T, f workloadGrantFixture, subject string) string {
	t.Helper()

	return oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", oauthtest.WorkloadClaims(f.issuer, subject, f.advertisedIssuer))
}

// A channel in the admitted Anthropic organization signs in through the rule
// the guided setup writes, as the agent assigned to it, and its session is
// minted for its own subject rather than the rule.
func TestCatalogClaudeTagRule_AdmitsAChannelInTheOrganization(t *testing.T) {
	t.Parallel()

	f := newCatalogWorkloadGrantFixture(t)

	w := f.exchange(t, claudeTagAssertion(t, f, f.subject), f.resource)
	f.requireWorkloadSession(t, w, f.advertisedIssuer)
	require.Len(t, f.workloadSessions(t), 1)
}

// A channel created later in the same organization needs no new rule.
func TestCatalogClaudeTagRule_AdmitsANewChannel(t *testing.T) {
	t.Parallel()

	f := newCatalogWorkloadGrantFixture(t)
	f.subject = "wimse://identity.anthropic.com/org/" + catalogTestOrganization + "/agent/agent-channel-2"

	w := f.exchange(t, claudeTagAssertion(t, f, f.subject), f.resource)
	f.requireWorkloadSession(t, w, f.advertisedIssuer)
}

// Every Anthropic organization's tokens come from the same issuer, so the rule
// must pin the organization: another customer's channel is refused.
func TestCatalogClaudeTagRule_RefusesAnotherOrganization(t *testing.T) {
	t.Parallel()

	f := newCatalogWorkloadGrantFixture(t)
	other := "wimse://identity.anthropic.com/org/org-456/agent/agent-channel-1"

	requireWorkloadGrantRefused(t, f.exchange(t, claudeTagAssertion(t, f, other), f.resource))
}

// The stem ends at "/agent/", so an organization whose ID merely begins with
// the admitted one is not covered.
func TestCatalogClaudeTagRule_RefusesAnOrganizationSharingThePrefix(t *testing.T) {
	t.Parallel()

	f := newCatalogWorkloadGrantFixture(t)
	collision := "wimse://identity.anthropic.com/org/" + catalogTestOrganization + "x/agent/agent-channel-1"

	requireWorkloadGrantRefused(t, f.exchange(t, claudeTagAssertion(t, f, collision), f.resource))
}
