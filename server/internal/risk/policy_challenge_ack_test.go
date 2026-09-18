package risk_test

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The warn acknowledgement link is the one affordance every transport hands a
// warned user, so this exercises it end to end and independently of any of
// them: mint a link the way a denying transport does, redeem it the way the
// dashboard does, and confirm the retry is no longer challenged.
func TestRiskPolicyChallenge_AcknowledgementClearsTheIdenticalRetry(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policy, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             uuid.New(),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "secret detection",
		Sources:        []string{"gitleaks"},
		Enabled:        true,
		Action:         "warn",
		AudienceType:   "everyone",
	})
	require.NoError(t, err)

	scanner, err := risk.NewScanner(
		testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn,
		newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, nil, &feature.InMemory{}, testCELEngine(t),
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
	)
	require.NoError(t, err)

	const (
		toolName    = "Bash"
		fingerprint = "fingerprint-example"
	)
	require.False(t, scanner.HasAcknowledgedChallenge(ctx, *authCtx.ProjectID, authCtx.UserID, policy.ID.String(), toolName, fingerprint),
		"nothing is acknowledged before the user approves")

	// What a denying transport does: record the challenge and mint the link.
	scanner.RecordPolicyChallenge(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, policy.ID.String(), toolName, policy.Name, "stripe", "stripe-key", fingerprint)
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	ackURL, _, err := risk.GeneratePolicyAckURL(ctx, ti.cacheAdapter, siteURL, risk.PolicyAckTokenInput{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        authCtx.ProjectID.String(),
		UserID:           authCtx.UserID,
		RiskPolicyID:     policy.ID.String(),
		PolicyName:       policy.Name,
		ToolName:         new(toolName),
		CallFingerprint:  fingerprint,
		ChallengeMessage: "A secret was detected in this session.",
		RememberFor:      0,
	}, 0)
	require.NoError(t, err)

	// What the user does: open the link and approve.
	ackToken := ackTokenFromURL(t, ackURL)
	peek, err := ti.service.GetRiskPolicyChallenge(ctx, &gen.GetRiskPolicyChallengePayload{SessionToken: nil, AckToken: ackToken})
	require.NoError(t, err)
	require.False(t, peek.Acknowledged)
	require.Equal(t, "A secret was detected in this session.", peek.Message)

	result, err := ti.service.AcknowledgeRiskPolicyChallenge(ctx, &gen.AcknowledgeRiskPolicyChallengePayload{SessionToken: nil, AckToken: ackToken})
	require.NoError(t, err)
	require.True(t, result.Acknowledged)

	require.True(t, scanner.HasAcknowledgedChallenge(ctx, *authCtx.ProjectID, authCtx.UserID, policy.ID.String(), toolName, fingerprint),
		"the identical retry now passes")
	require.False(t, scanner.HasAcknowledgedChallenge(ctx, *authCtx.ProjectID, authCtx.UserID, policy.ID.String(), toolName, "a-different-call"),
		"a different call is still challenged")
}

func TestRiskPolicyChallenge_DeclineKeepsTheRetryBlocked(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policy, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:             uuid.New(),
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "secret detection",
		Sources:        []string{"gitleaks"},
		Enabled:        true,
		Action:         "warn",
		AudienceType:   "everyone",
	})
	require.NoError(t, err)

	scanner, err := risk.NewScanner(
		testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn,
		newTestCustomRuleAnalyzer(t, ti.conn), nil, nil, nil, &feature.InMemory{}, testCELEngine(t),
		metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()),
	)
	require.NoError(t, err)

	const fingerprint = "fingerprint-example"
	scanner.RecordPolicyChallenge(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID, policy.ID.String(), "", policy.Name, "stripe", "stripe-key", fingerprint)
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	ackURL, _, err := risk.GeneratePolicyAckURL(ctx, ti.cacheAdapter, siteURL, risk.PolicyAckTokenInput{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        authCtx.ProjectID.String(),
		UserID:           authCtx.UserID,
		RiskPolicyID:     policy.ID.String(),
		PolicyName:       policy.Name,
		ToolName:         nil,
		CallFingerprint:  fingerprint,
		ChallengeMessage: "A secret was detected in this session.",
		RememberFor:      0,
	}, 0)
	require.NoError(t, err)

	ackToken := ackTokenFromURL(t, ackURL)
	declined, err := ti.service.DeclineRiskPolicyChallenge(ctx, &gen.DeclineRiskPolicyChallengePayload{SessionToken: nil, AckToken: ackToken})
	require.NoError(t, err)
	require.True(t, declined.Declined)

	require.False(t, scanner.HasAcknowledgedChallenge(ctx, *authCtx.ProjectID, authCtx.UserID, policy.ID.String(), "", fingerprint))
	_, err = ti.service.AcknowledgeRiskPolicyChallenge(ctx, &gen.AcknowledgeRiskPolicyChallengePayload{SessionToken: nil, AckToken: ackToken})
	require.Error(t, err, "a declined link can no longer be approved")
}

// ackTokenFromURL reads the token out of the link's fragment, the way the
// dashboard's acknowledgement page does.
func ackTokenFromURL(t *testing.T, ackURL string) string {
	t.Helper()
	parsed, err := url.Parse(ackURL)
	require.NoError(t, err)
	values, err := url.ParseQuery(parsed.Fragment)
	require.NoError(t, err)
	token := values.Get("ack_token")
	require.NotEmpty(t, token)
	return token
}
