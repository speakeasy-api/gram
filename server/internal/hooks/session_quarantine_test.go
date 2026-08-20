package hooks

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestIngestSessionQuarantineTripsAndFreezesPromptAndTool(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	sessionID := "quarantine-session-" + uuid.NewString()
	userMessage := "Your organization quarantined this session."
	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:       "quarantine",
		PolicyName:   "Sensitive commands",
		Description:  "matched a restricted command",
		MatchedValue: "restricted",
		UserMessage:  &userMessage,
	}}

	triggerPayload := canonicalIngestPayload("claude", "prompt.submitted", sessionID)
	promptText := "restricted prompt"
	triggerPayload.Data = &gen.HookIngestData{Prompt: &gen.HookPromptData{Text: &promptText}}
	triggered, err := ti.service.Ingest(ctx, triggerPayload)
	require.NoError(t, err)
	require.Equal(t, "deny", triggered.Decision)
	require.Equal(t, userMessage, requireString(t, triggered.Message))

	ti.service.riskScanner = &stubResultScanner{}
	fixedMessage := "This session has been quarantined by your organization's Speakeasy risk policy \"Sensitive commands\". Contact your org admin to release it."

	prompt, err := ti.service.Ingest(ctx, canonicalIngestPayload("custom-adapter", "prompt.submitted", sessionID))
	require.NoError(t, err)
	require.Equal(t, "deny", prompt.Decision)
	require.Equal(t, fixedMessage, requireString(t, prompt.Message))

	tool, err := ti.service.Ingest(ctx, canonicalIngestPayload("custom-adapter", "tool.requested", sessionID))
	require.NoError(t, err)
	require.Equal(t, "deny", tool.Decision)
	require.Equal(t, fixedMessage, requireString(t, tool.Message))

	row, err := riskrepo.New(ti.conn).GetActiveSessionQuarantineBySession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, "Sensitive commands", row.RiskPolicyName)

	auditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSessionQuarantineOpen)
	require.NoError(t, err)
	require.Equal(t, int64(1), auditCount)
}

func TestIngestSessionQuarantineRedisErrorFailMode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = &stubResultScanner{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := "quarantine-cache-error-" + uuid.NewString()
	ti.service.cache = mcpGetErrorCache{
		Cache:   ti.service.cache,
		failKey: "session:quarantine:" + sessionID,
		err:     errors.New("redis unavailable"),
	}

	failOpen, err := ti.service.Ingest(ctx, canonicalIngestPayload("claude", "prompt.submitted", sessionID))
	require.NoError(t, err)
	require.Equal(t, "allow", failOpen.Decision)

	require.NoError(t, riskrepo.New(ti.conn).SetSessionQuarantineFailClosed(ctx, riskrepo.SetSessionQuarantineFailClosedParams{
		FailClosed:     true,
		OrganizationID: authCtx.ActiveOrganizationID,
	}))

	failClosed, err := ti.service.Ingest(ctx, canonicalIngestPayload("claude", "prompt.submitted", sessionID))
	require.NoError(t, err)
	require.Equal(t, "deny", failClosed.Decision)
	require.Contains(t, requireString(t, failClosed.Message), "policy \"unknown\"")

	cleanMiss, err := ti.service.Ingest(ctx, canonicalIngestPayload("claude", "prompt.submitted", "quarantine-clean-miss-"+uuid.NewString()))
	require.NoError(t, err)
	require.Equal(t, "allow", cleanMiss.Decision)
}

func requireString(t *testing.T, value *string) string {
	t.Helper()
	require.NotNil(t, value)
	return *value
}
