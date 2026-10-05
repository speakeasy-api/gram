package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	srv "github.com/speakeasy-api/gram/server/gen/http/hooks/server"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

const (
	// legacyCurlUserAgent is what a legacy hook script's curl sends.
	legacyCurlUserAgent = "curl/8.7.1"

	// retiredObservabilityModeFeature is the organization_features name of
	// the removed observability mode.
	retiredObservabilityModeFeature = "observability_mode"
)

// passThroughAuthorizer admits every plugin credential and keeps the request
// context, which already carries the test's auth context.
type passThroughAuthorizer struct{}

func (passThroughAuthorizer) Authorize(ctx context.Context, _ string, _ *security.APIKeyScheme) (context.Context, error) {
	return ctx, nil
}

// failingClaimCache fails every set-if-absent write, as an unreachable Redis
// would, and passes every other call through.
type failingClaimCache struct {
	cache.Cache
}

func (failingClaimCache) Add(context.Context, string, time.Duration) (bool, error) {
	return false, errors.New("redis unavailable")
}

// enableLegacyNotice makes plugin credentials authenticate and turns the
// notice flag on for the test organization. It returns the request context a
// legacy curl hook script produces.
func enableLegacyNotice(t *testing.T, ctx context.Context, ti *testInstance) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagHooksLegacyOutOfDateNotice, authCtx.ActiveOrganizationID, true)
	ti.service.features = flags
	ti.service.auth = passThroughAuthorizer{}
	return withUserAgent(ctx, legacyCurlUserAgent)
}

// newLegacyNoticeService wires a hooks service that allows every event, with
// the notice enabled, and returns a legacy curl request context.
func newLegacyNoticeService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = hooksPostureFeatures{failOpen: true}
	ti.service.riskScanner = &stubResultScanner{}
	return enableLegacyNotice(t, ctx, ti), ti
}

func withUserAgent(ctx context.Context, userAgent string) context.Context {
	return contextvalues.SetRequestContext(ctx, &contextvalues.RequestContext{UserAgent: userAgent})
}

// legacyPluginPayload is a hook event as a generated curl plugin sends it,
// with its baked Gram-Key and Gram-Project headers.
func legacyPluginPayload(hookEventName, sessionID string) *gen.ClaudePayload {
	key := "gram_local_legacy_plugin"
	projectSlug := "project"
	userEmail := "legacy-plugin@example.com"
	payload := &gen.ClaudePayload{
		HookEventName:    hookEventName,
		SessionID:        &sessionID,
		ApikeyToken:      &key,
		ProjectSlugInput: &projectSlug,
		UserEmail:        &userEmail,
	}
	switch HookEvent(hookEventName) {
	case HookEventUserPromptSubmit:
		prompt := "summarize the readme"
		payload.Prompt = &prompt
	case HookEventPreToolUse, HookEventPostToolUse, HookEventPostToolUseFailure:
		toolName := "Read"
		toolUseID := "toolu_" + uuid.NewString()
		payload.ToolName = &toolName
		payload.ToolUseID = &toolUseID
		payload.ToolInput = map[string]any{"file_path": "/tmp/readme.md"}
	default:
	}
	return payload
}

func withHostname(payload *gen.ClaudePayload, hostname string) *gen.ClaudePayload {
	payload.HookHostname = &hostname
	return payload
}

// claudeResponseJSON is the body the endpoint writes for res.
func claudeResponseJSON(t *testing.T, res *gen.ClaudeHookResult) string {
	t.Helper()
	body, err := json.Marshal(srv.NewClaudeResponseBody(res))
	require.NoError(t, err)
	return string(body)
}

func requireNoLegacyNotice(t *testing.T, res *gen.ClaudeHookResult) {
	t.Helper()
	require.NotNil(t, res)
	if res.SystemMessage != nil {
		require.NotEqual(t, legacyHooksOutOfDateNotice, *res.SystemMessage)
	}
}

func TestClaude_LegacyNotice_UserPromptSubmitExactResponse(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)

	res, err := ti.service.Claude(ctx, legacyPluginPayload("UserPromptSubmit", uuid.NewString()))
	require.NoError(t, err)
	require.JSONEq(t, `{"systemMessage":"Speakeasy: your hooks plugin is out of date. Ask your Claude Code admin to update the managed settings so the Speakeasy plugin can auto-update."}`, claudeResponseJSON(t, res))
	require.Less(t, len(legacyHooksOutOfDateNotice), 200, "the notice must stay short")
}

// The pass-through answer the decision budget gives a fail-open organization
// is today's PreToolUse allow shape, so the notice rides on it unchanged.
func TestClaude_LegacyNotice_PreToolUseExactResponseOnBudgetFailOpen(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, true, blockingScanResult())
	ctx = enableLegacyNotice(t, ctx, ti)

	res, err := ti.service.Claude(ctx, legacyPluginPayload("PreToolUse", uuid.NewString()))
	require.NoError(t, err)
	requireScanStarted(t, scanner)
	require.JSONEq(t, `{"systemMessage":"Speakeasy: your hooks plugin is out of date. Ask your Claude Code admin to update the managed settings so the Speakeasy plugin can auto-update.","hookSpecificOutput":{"hookEventName":"PreToolUse"}}`, claudeResponseJSON(t, res))
}

// The notice never adds or changes a permission decision: a native tool call
// keeps the allow the handler returned.
func TestClaude_LegacyNotice_PreToolUseKeepsHandlerVerdict(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)

	res, err := ti.service.Claude(ctx, legacyPluginPayload("PreToolUse", uuid.NewString()))
	require.NoError(t, err)
	require.JSONEq(t, `{"systemMessage":"Speakeasy: your hooks plugin is out of date. Ask your Claude Code admin to update the managed settings so the Speakeasy plugin can auto-update.","hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}`, claudeResponseJSON(t, res))
}

// Events that run async in some legacy build would hand the notice to the
// model, and SessionStart responses are discarded, so none of them carry it.
// They also leave the session's claim for the next synchronous event.
func TestClaude_LegacyNotice_SkipsOtherEvents(t *testing.T) {
	t.Parallel()
	for _, hookEventName := range []string{"PostToolUse", "PostToolUseFailure", "SessionStart", "Stop", "SessionEnd", "Notification"} {
		t.Run(hookEventName, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newLegacyNoticeService(t)
			sessionID := uuid.NewString()

			res, err := ti.service.Claude(ctx, legacyPluginPayload(hookEventName, sessionID))
			require.NoError(t, err)
			requireNoLegacyNotice(t, res)

			res, err = ti.service.Claude(ctx, legacyPluginPayload("UserPromptSubmit", sessionID))
			require.NoError(t, err)
			require.NotNil(t, res.SystemMessage, "the session's claim is still available")
			require.Equal(t, legacyHooksOutOfDateNotice, *res.SystemMessage)
		})
	}
}

func TestClaude_LegacyNotice_SkipsBlockedPrompt(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)
	ti.service.riskScanner = &stubResultScanner{result: blockingScanResult()}
	sessionID := uuid.NewString()

	res, err := ti.service.Claude(ctx, legacyPluginPayload("UserPromptSubmit", sessionID))
	require.NoError(t, err)
	require.NotNil(t, res.Decision)
	require.Equal(t, "block", *res.Decision)
	require.Nil(t, res.SystemMessage)

	ti.service.riskScanner = &stubResultScanner{}
	res, err = ti.service.Claude(ctx, legacyPluginPayload("UserPromptSubmit", sessionID))
	require.NoError(t, err)
	require.NotNil(t, res.SystemMessage, "a blocked event does not use up the session's notice")
	require.Equal(t, legacyHooksOutOfDateNotice, *res.SystemMessage)
}

func TestClaude_LegacyNotice_SkipsBlockedToolCall(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)
	ti.service.riskScanner = &stubResultScanner{result: blockingScanResult()}

	res, err := ti.service.Claude(ctx, legacyPluginPayload("PreToolUse", uuid.NewString()))
	require.NoError(t, err)
	output, ok := res.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.Equal(t, "deny", *output.PermissionDecision)
	requireNoLegacyNotice(t, res)
}

// A fail-closed organization's budget answer is a block, so it keeps its own
// timeout message.
func TestClaude_LegacyNotice_SkipsBudgetFailClosedBlock(t *testing.T) {
	t.Parallel()
	ctx, ti, scanner := newBudgetedClaudeService(t, false, nil)
	ctx = enableLegacyNotice(t, ctx, ti)

	res, err := ti.service.Claude(ctx, legacyPluginPayload("PreToolUse", uuid.NewString()))
	require.NoError(t, err)
	requireScanStarted(t, scanner)
	require.NotNil(t, res.SystemMessage)
	require.Contains(t, *res.SystemMessage, "did not finish in time")
	requireNoLegacyNotice(t, res)
}

// Any response that already decides something, or already speaks to the
// user, is returned untouched.
func TestMaybeAttachLegacyNotice_LeavesDecidedResponsesUntouched(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)

	withSystemMessage := makeHookResult("UserPromptSubmit")
	withSystemMessage.SystemMessage = new("an existing warning")
	stopped := makeHookResult("UserPromptSubmit")
	stopped.Continue = new(false)
	asked := makeHookResult("PreToolUse")
	askedOutput, ok := asked.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	askedOutput.PermissionDecision = new("ask")

	cases := []struct {
		name string
		res  *gen.ClaudeHookResult
	}{
		{name: "existing system message", res: withSystemMessage},
		{name: "continue false", res: stopped},
		{name: "permission ask", res: asked},
		{name: "prompt block", res: constructBlockResponse("UserPromptSubmit", "blocked by policy")},
		{name: "tool call deny", res: constructBlockResponse("PreToolUse", "blocked by policy")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hookEventName := "UserPromptSubmit"
			if _, ok := tc.res.HookSpecificOutput.(*HookSpecificOutput); ok {
				hookEventName = "PreToolUse"
			}
			before := claudeResponseJSON(t, tc.res)
			ti.service.maybeAttachLegacyNotice(ctx, ti.service.logger, legacyPluginPayload(hookEventName, uuid.NewString()), tc.res, true)
			require.Equal(t, before, claudeResponseJSON(t, tc.res))
		})
	}
}

func TestClaude_LegacyNotice_SkipsNonCurlClients(t *testing.T) {
	t.Parallel()
	for _, userAgent := range []string{"", "Go-http-client/1.1", "speakeasy-sdk/go 0.1.0", "Mozilla/5.0 (compatible; curl/8.7.1)"} {
		t.Run(userAgent, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newLegacyNoticeService(t)

			res, err := ti.service.Claude(withUserAgent(ctx, userAgent), legacyPluginPayload("UserPromptSubmit", uuid.NewString()))
			require.NoError(t, err)
			require.Nil(t, res.SystemMessage)
		})
	}
}

// Builds that send no plugin credentials include the standalone scripts that
// run prompts async, so an unauthenticated request never gets the notice.
func TestClaude_LegacyNotice_SkipsUnauthenticatedScripts(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)
	payload := legacyPluginPayload("UserPromptSubmit", uuid.NewString())
	payload.ApikeyToken = nil
	payload.ProjectSlugInput = nil

	res, err := ti.service.Claude(ctx, payload)
	require.NoError(t, err)
	require.Nil(t, res.SystemMessage)
}

// Plugins generated in observability mode ran every event async. The mode was
// retired by soft-deleting its rows, which still count.
func TestClaude_LegacyNotice_SkipsOrganizationsThatHadObservabilityMode(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features := productfeaturesrepo.New(ti.conn)
	_, err := features.EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    retiredObservabilityModeFeature,
	})
	require.NoError(t, err)
	_, err = features.DeleteFeature(ctx, productfeaturesrepo.DeleteFeatureParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		FeatureName:    retiredObservabilityModeFeature,
	})
	require.NoError(t, err)

	for _, hookEventName := range []string{"UserPromptSubmit", "PreToolUse"} {
		res, err := ti.service.Claude(ctx, legacyPluginPayload(hookEventName, uuid.NewString()))
		require.NoError(t, err)
		requireNoLegacyNotice(t, res)
	}
}

func TestClaude_LegacyNotice_OncePerSession(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)
	sessionID := uuid.NewString()

	res, err := ti.service.Claude(ctx, legacyPluginPayload("UserPromptSubmit", sessionID))
	require.NoError(t, err)
	require.NotNil(t, res.SystemMessage)
	require.Equal(t, legacyHooksOutOfDateNotice, *res.SystemMessage)

	for _, hookEventName := range []string{"PreToolUse", "UserPromptSubmit", "PreToolUse"} {
		res, err := ti.service.Claude(ctx, legacyPluginPayload(hookEventName, sessionID))
		require.NoError(t, err)
		require.Nil(t, res.SystemMessage, "%s repeated the notice", hookEventName)
	}

	res, err = ti.service.Claude(ctx, legacyPluginPayload("PreToolUse", uuid.NewString()))
	require.NoError(t, err)
	require.NotNil(t, res.SystemMessage, "a new session gets its own notice")
}

// A device that just saw the notice is not told again in its next session.
func TestClaude_LegacyNotice_OncePerDeviceWindow(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)
	hostname := "legacy-device-" + uuid.NewString()

	res, err := ti.service.Claude(ctx, withHostname(legacyPluginPayload("UserPromptSubmit", uuid.NewString()), hostname))
	require.NoError(t, err)
	require.NotNil(t, res.SystemMessage)
	require.Equal(t, legacyHooksOutOfDateNotice, *res.SystemMessage)

	res, err = ti.service.Claude(ctx, withHostname(legacyPluginPayload("UserPromptSubmit", uuid.NewString()), hostname))
	require.NoError(t, err)
	require.Nil(t, res.SystemMessage, "the same device is throttled across sessions")

	res, err = ti.service.Claude(ctx, withHostname(legacyPluginPayload("UserPromptSubmit", uuid.NewString()), "legacy-device-"+uuid.NewString()))
	require.NoError(t, err)
	require.NotNil(t, res.SystemMessage, "another device gets its own notice")
}

func TestClaude_LegacyNotice_RedisFailureSkipsNoticeAndAnswers(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)
	ti.service.cache = failingClaimCache{Cache: ti.service.cache}

	for _, hookEventName := range []string{"UserPromptSubmit", "PreToolUse"} {
		res, err := ti.service.Claude(ctx, legacyPluginPayload(hookEventName, uuid.NewString()))
		require.NoError(t, err, "a failed claim must never fail the hook")
		require.Nil(t, res.SystemMessage)
	}
}

func TestClaude_LegacyNotice_FlagOff(t *testing.T) {
	t.Parallel()
	ctx, ti := newLegacyNoticeService(t)
	ti.service.features = &feature.InMemory{}

	for _, hookEventName := range []string{"UserPromptSubmit", "PreToolUse"} {
		res, err := ti.service.Claude(ctx, legacyPluginPayload(hookEventName, uuid.NewString()))
		require.NoError(t, err)
		require.Nil(t, res.SystemMessage)
	}
}
