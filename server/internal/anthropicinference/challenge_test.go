package anthropicinference

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newChallengeService(t *testing.T, store transcriptStore, scanner scanner) *Service {
	t.Helper()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	return &Service{
		store:   store,
		scanner: scanner,
		logger:  testenv.NewLogger(t),
		cache:   cache.NewRedisCacheAdapter(redisClient),
		siteURL: siteURL,
	}
}

func toolUseMessage(tool, input string) Message {
	return Message{Role: "assistant", Content: json.RawMessage(fmt.Sprintf(`[{"type":"tool_use","tool_name":%q,"input":%s}]`, tool, input))}
}

func warnResult() *risk.ScanResult {
	return &risk.ScanResult{
		Action:          "warn",
		PolicyID:        uuid.NewString(),
		PolicyName:      "secret detection",
		Source:          "gitleaks",
		MessageType:     "user",
		RuleID:          "stripe-key",
		Description:     "Stripe live key",
		UserMessage:     new("A secret was detected in this session."),
		MatchedValue:    "sk_live_example",
		Entity:          "stripe",
		CallFingerprint: "fingerprint-example",
	}
}

func TestServiceWarnDenyCarriesAcknowledgementLink(t *testing.T) {
	t.Parallel()
	store := &memoryStore{userID: "user-example"}
	scanner := &recordingScanner{result: warnResult()}
	service := newChallengeService(t, store, scanner)

	verdict, err := service.Process(t.Context(), Config{OrganizationID: "org_example", ProjectID: uuid.New()}, exampleFrame())
	require.NoError(t, err)

	require.Equal(t, "deny", verdict.Action)
	assert.Contains(t, verdict.DenyReason, "A secret was detected in this session.", "keeps the policy's own copy")
	assert.Contains(t, verdict.DenyReason, "https://app.example.test/risk-policy-challenge/acknowledge#ack_token=rpak1.")
	assert.True(t, scanner.recordedChallenge, "the challenge is recorded so the link can be redeemed")
	assert.Empty(t, store.accepted, "a denied frame never becomes accepted history")
}

func TestServiceAllowsAcknowledgedWarn(t *testing.T) {
	t.Parallel()
	store := &memoryStore{userID: "user-example"}
	scanner := &recordingScanner{result: warnResult(), acknowledged: true}
	service := newChallengeService(t, store, scanner)

	verdict, err := service.Process(t.Context(), Config{OrganizationID: "org_example", ProjectID: uuid.New()}, exampleFrame())
	require.NoError(t, err)

	assert.Equal(t, "allow", verdict.Action)
	assert.False(t, scanner.recordedChallenge, "an approved call is not challenged again")
	assert.NotEmpty(t, store.accepted, "the approved transcript becomes accepted history, so the conversation stays usable")
}

func TestServiceWarnDeniesWithoutLinkWhenTheUserIsUnknown(t *testing.T) {
	t.Parallel()
	// An unresolved actor has nobody to bind an acknowledgement to. A warn must
	// still deny rather than allow.
	store := &memoryStore{userID: ""}
	scanner := &recordingScanner{result: warnResult()}
	service := newChallengeService(t, store, scanner)

	verdict, err := service.Process(t.Context(), Config{OrganizationID: "org_example", ProjectID: uuid.New()}, exampleFrame())
	require.NoError(t, err)

	require.Equal(t, "deny", verdict.Action)
	assert.Equal(t, "A secret was detected in this session.", verdict.DenyReason)
	assert.NotContains(t, verdict.DenyReason, "ack_token")
	assert.False(t, scanner.recordedChallenge)
}

func TestServiceBlockDenyCarriesNoAcknowledgementLink(t *testing.T) {
	t.Parallel()
	result := warnResult()
	result.Action = "block"
	store := &memoryStore{userID: "user-example"}
	scanner := &recordingScanner{result: result}
	service := newChallengeService(t, store, scanner)

	verdict, err := service.Process(t.Context(), Config{OrganizationID: "org_example", ProjectID: uuid.New()}, exampleFrame())
	require.NoError(t, err)

	require.Equal(t, "deny", verdict.Action)
	assert.NotContains(t, verdict.DenyReason, "ack_token", "a block is not appealable")
	assert.False(t, scanner.recordedChallenge)
}

func TestServiceWarnLinkNamesTheToolThatWasChallenged(t *testing.T) {
	t.Parallel()
	store := &memoryStore{userID: "user-example"}
	scanner := &recordingScanner{result: warnResult()}
	service := newChallengeService(t, store, scanner)
	frame := exampleFrame()
	frame.Messages = []Message{toolUseMessage("bash", `{"cmd":"cat .env"}`)}

	_, err := service.Process(t.Context(), Config{OrganizationID: "org_example", ProjectID: uuid.New()}, frame)
	require.NoError(t, err)

	assert.Equal(t, []string{"bash"}, scanner.challengedTools)
}

func TestRenderChallengeDenyReasonKeepsTheLinkWithinTheProtocolBound(t *testing.T) {
	t.Parallel()
	ackURL := "https://app.example.test/risk-policy-challenge/acknowledge#ack_token=rpak1.AAAAAAAAAAAAAAAAAAAAAA"

	short := renderChallengeDenyReason("blocked", ackURL)
	assert.Equal(t, "blocked\n\n"+ackInstruction+ackURL, short)

	// The handler truncates deny_reason from the right, so an overlong policy
	// message must lose its own tail rather than the link.
	long := renderChallengeDenyReason(strings.Repeat("x", 4000), ackURL)
	assert.LessOrEqual(t, len([]rune(long)), maxDenyReasonRunes)
	assert.True(t, strings.HasSuffix(long, ackURL), "the link survives truncation")
	assert.Contains(t, long, "…", "the truncated message is marked as cut")
}

func TestDenyBodyFallsBackWhenThePolicyHasNoMessage(t *testing.T) {
	t.Parallel()
	assert.Equal(t, defaultDenyReason, denyBody(&risk.ScanResult{}))
	assert.Equal(t, defaultDenyReason, denyBody(&risk.ScanResult{UserMessage: new("   ")}))
	assert.Equal(t, "custom", denyBody(&risk.ScanResult{UserMessage: new(" custom ")}))
}
