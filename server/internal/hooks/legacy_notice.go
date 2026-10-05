package hooks

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// The legacy hooks out-of-date notice tells users of the curl hook plugins
// that still call /rpc/hooks.claude that their plugin is out of date. Those
// plugins never auto-update because their Claude Code managed settings point
// at the wrong marketplace, and only an admin can fix that.
//
// The notice travels as the response's systemMessage. The legacy scripts echo
// a 2xx body to stdout, and Claude Code shows a systemMessage from a
// synchronous hook to the user without blocking. From an async hook it hands
// the text to the model instead, so the notice only goes to events and builds
// known to run synchronously:
//
//   - Events: only UserPromptSubmit and PreToolUse. The generated plugins run
//     PostToolUse, PostToolUseFailure, SessionEnd and Notification async,
//     every build discards the SessionStart response, and Stop is skipped to
//     limit noise.
//   - Builds: the request must authenticate with Gram-Key and Gram-Project.
//     The standalone send_hook.sh 0.0.6 to 0.0.8, which run UserPromptSubmit
//     or PreToolUse async, send no credentials. Every build that sends them
//     runs both events synchronously, except plugins generated for an
//     organization in observability mode, which ran every event async.
//     Organizations that ever enabled that mode are skipped.
const (
	// legacyHooksOutOfDateNotice is the text the user sees. Claude Code labels
	// the line with the hook event name, not the plugin, so it names Speakeasy.
	legacyHooksOutOfDateNotice = "Speakeasy: your hooks plugin is out of date. Ask your Claude Code admin to update the managed settings so the Speakeasy plugin can auto-update."

	// legacyCurlUserAgentPrefix starts the User-Agent every legacy hook
	// script sends. The hooks binary posts to /rpc/hooks.ingest with its own.
	legacyCurlUserAgentPrefix = "curl/"

	// legacyNoticeSessionTTL holds the once-per-session claim. It outlives a
	// working day of one Claude Code session; a session resumed after that
	// may see the notice once more.
	legacyNoticeSessionTTL = 24 * time.Hour

	// legacyNoticeDeviceTTL spaces notices on one machine, so a user who opens
	// many short sessions sees it about twice a working day instead of in
	// every session.
	legacyNoticeDeviceTTL = 6 * time.Hour

	// legacyNoticeTimeout bounds the Redis claims and the observability-mode
	// read. They run after the verdict, inside the 10s the legacy client
	// waits, so they must not use up the headroom the decision budget leaves.
	legacyNoticeTimeout = 500 * time.Millisecond
)

// maybeAttachLegacyNotice sets the out-of-date notice as res.SystemMessage
// when the caller is a legacy curl hook client known to run the event
// synchronously, at most once per Claude session and per device window. It
// never changes the verdict: it only fills an empty systemMessage on a
// pass-through response, and any failure leaves the response unchanged.
// pluginAuthed reports whether the request authenticated with Gram-Key and
// Gram-Project, which ctx then carries.
func (s *Service) maybeAttachLegacyNotice(ctx context.Context, logger *slog.Logger, payload *gen.ClaudePayload, res *gen.ClaudeHookResult, pluginAuthed bool) {
	if !pluginAuthed || !legacyNoticeEligibleResponse(payload.HookEventName, res) || !isLegacyCurlClient(ctx) {
		return
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return
	}
	sessionID := strings.TrimSpace(conv.PtrValOr(payload.SessionID, ""))
	if sessionID == "" || !s.legacyNoticeEnabled(ctx, authCtx) {
		return
	}

	noticeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), legacyNoticeTimeout)
	defer cancel()

	// Claim the session first: every later event of the session stops at this
	// one Redis write, so the observability-mode read runs once per session.
	if !s.claimLegacyNotice(noticeCtx, logger, legacyNoticeSessionCacheKey(sessionID), legacyNoticeSessionTTL) {
		return
	}
	everObservability, err := s.repo.OrganizationEverEnabledObservabilityMode(noticeCtx, authCtx.ActiveOrganizationID)
	if err != nil {
		logger.WarnContext(ctx, "read observability mode history; skipping the legacy hooks notice",
			attr.SlogEvent("claude_hook_legacy_notice_lookup_failed"),
			attr.SlogError(err),
		)
		return
	}
	if everObservability {
		logger.InfoContext(ctx, "skipped the legacy hooks notice: the organization's plugins may run every event async",
			attr.SlogEvent("claude_hook_legacy_notice_skipped_observability_mode"),
		)
		return
	}
	if hostname := strings.TrimSpace(conv.PtrValOr(payload.HookHostname, "")); hostname != "" {
		if !s.claimLegacyNotice(noticeCtx, logger, legacyNoticeDeviceCacheKey(authCtx.ActiveOrganizationID, hostname), legacyNoticeDeviceTTL) {
			return
		}
	}

	notice := legacyHooksOutOfDateNotice
	res.SystemMessage = &notice
	logger.InfoContext(ctx, "attached the legacy hooks out-of-date notice",
		attr.SlogEvent("claude_hook_legacy_notice_attached"),
		attr.SlogHookHostname(conv.PtrValOr(payload.HookHostname, "")),
	)
}

// legacyNoticeEligibleResponse reports whether res can carry the notice: a
// UserPromptSubmit or PreToolUse pass-through with no systemMessage of its
// own. A block, a deny or ask, or continue:false keeps its response as is.
func legacyNoticeEligibleResponse(hookEventName string, res *gen.ClaudeHookResult) bool {
	if res == nil {
		return false
	}
	event := HookEvent(hookEventName)
	if event != HookEventUserPromptSubmit && event != HookEventPreToolUse {
		return false
	}
	if conv.PtrValOr(res.SystemMessage, "") != "" {
		return false
	}
	if res.Continue != nil && !*res.Continue {
		return false
	}
	return claudeHookDecision(res) == hookMetricDecisionAllow
}

// isLegacyCurlClient reports whether the request came from a curl-based hook
// script.
func isLegacyCurlClient(ctx context.Context) bool {
	requestContext, ok := contextvalues.GetRequestContext(ctx)
	return ok && requestContext != nil && strings.HasPrefix(requestContext.UserAgent, legacyCurlUserAgentPrefix)
}

// legacyNoticeEnabled evaluates the rollout flag from local PostHog
// definitions only, so the hook path never waits on PostHog. Absent,
// inconclusive or failed evaluations read as off.
func (s *Service) legacyNoticeEnabled(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	if s.features == nil {
		return false
	}
	groups := feature.OrgProjectGroups(authCtx.OrganizationSlug, conv.PtrValOr(authCtx.ProjectSlug, ""))
	enabled, err := s.features.IsFlagEnabledLocal(ctx, feature.FlagHooksLegacyOutOfDateNotice, authCtx.ActiveOrganizationID, groups, nil)
	if err != nil {
		s.logger.WarnContext(ctx, "evaluate the legacy hooks notice flag; treating it as off",
			attr.SlogEvent("claude_hook_legacy_notice_flag_failed"),
			attr.SlogError(err),
		)
		return false
	}
	return enabled
}

// claimLegacyNotice takes key with a set-if-absent write and reports whether
// this request won it. A cache error skips the notice: it is never worth
// failing or delaying the hook over.
func (s *Service) claimLegacyNotice(ctx context.Context, logger *slog.Logger, key string, ttl time.Duration) bool {
	claimed, err := s.cache.Add(ctx, key, ttl)
	if err != nil {
		logger.WarnContext(ctx, "claim the legacy hooks notice; skipping it",
			attr.SlogEvent("claude_hook_legacy_notice_claim_failed"),
			attr.SlogError(err),
		)
		return false
	}
	return claimed
}

// legacyNoticeSessionCacheKey marks a Claude session that already had its
// chance at the notice.
func legacyNoticeSessionCacheKey(sessionID string) string {
	return fmt.Sprintf("hook:legacy-notice:claude:%s", sessionID)
}

// legacyNoticeDeviceCacheKey marks a device, identified by the hostname the
// hook script sends, that saw the notice recently. The hostname is hashed so
// the key stays bounded and does not store it in clear.
func legacyNoticeDeviceCacheKey(organizationID, hostname string) string {
	digest := sha256.Sum256([]byte(organizationID + "\x00" + hostname))
	return fmt.Sprintf("hook:legacy-notice:claude:device:%x", digest)
}
