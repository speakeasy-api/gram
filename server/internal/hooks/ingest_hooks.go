package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hookevents"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolref"
)

const (
	hookIngestSchemaV1           = "hook.ingest.v1"
	agentTurnPrefix              = "agent-turn:v1:"
	agentPromptCorrelationPrefix = "agent-prompt:v1:"
)

type authenticatedIngestOptionsKey struct{}

// AuthenticatedIngestOptions controls trusted in-process ingestion behavior.
type AuthenticatedIngestOptions struct {
	AllowWarnAcknowledgement     bool
	AllowSessionIdentityFallback bool
	SourceAttributes             map[attr.Key]any
	OutputToolCalls              []any
}

// ResolvedActor is the exact actor selected by canonical hook attribution.
type ResolvedActor struct {
	UserID string
	Email  string
}

// AuthenticatedIngestResult includes the public hook result and trusted
// attribution details needed by in-process adapters.
type AuthenticatedIngestResult struct {
	Result *gen.IngestHookResult
	Actor  ResolvedActor
}

func defaultAuthenticatedIngestOptions() AuthenticatedIngestOptions {
	return AuthenticatedIngestOptions{
		AllowWarnAcknowledgement:     true,
		AllowSessionIdentityFallback: true,
		SourceAttributes:             nil,
		OutputToolCalls:              nil,
	}
}

func authenticatedIngestOptions(ctx context.Context) AuthenticatedIngestOptions {
	if options, ok := ctx.Value(authenticatedIngestOptionsKey{}).(AuthenticatedIngestOptions); ok {
		return options
	}
	return defaultAuthenticatedIngestOptions()
}

const (
	canonicalSessionCacheWriteTimeout = time.Second
	skillObservationWriteTimeout      = time.Second
)

// eventTypeSkillActivated is the canonical event type senders use when a
// provider reports a skill activation directly (Claude's Skill tool). Inferred
// activations (Codex heuristics) arrive as ordinary events carrying data.skill
// instead, and are distinguished by isExplicitSkillActivation.
const eventTypeSkillActivated = "skill.activated"

func isExplicitSkillActivation(payload *gen.IngestPayload) bool {
	return strings.TrimSpace(payload.Event.Type) == eventTypeSkillActivated
}

// IngestAuthenticated bypasses transport authentication for trusted in-process
// callers that have already authenticated the supplied organization and project.
func (s *Service) IngestAuthenticated(ctx context.Context, authCtx *contextvalues.AuthContext, payload *gen.IngestPayload) (*gen.IngestHookResult, error) {
	return s.IngestAuthenticatedWithOptions(ctx, authCtx, payload, defaultAuthenticatedIngestOptions())
}

// IngestAuthenticatedWithOptions bypasses transport authentication and applies
// behavior selected by a trusted in-process caller.
func (s *Service) IngestAuthenticatedWithOptions(ctx context.Context, authCtx *contextvalues.AuthContext, payload *gen.IngestPayload, options AuthenticatedIngestOptions) (*gen.IngestHookResult, error) {
	result, err := s.IngestAuthenticatedDetailed(ctx, authCtx, payload, options)
	if err != nil {
		return nil, err
	}
	return result.Result, nil
}

// IngestAuthenticatedDetailed bypasses transport authentication and returns
// the canonical actor resolved during ingestion to trusted in-process callers.
func (s *Service) IngestAuthenticatedDetailed(ctx context.Context, authCtx *contextvalues.AuthContext, payload *gen.IngestPayload, options AuthenticatedIngestOptions) (*AuthenticatedIngestResult, error) {
	if payload == nil {
		return nil, oops.E(oops.CodeInvalid, nil, "ingest payload is required")
	}
	if authCtx == nil || strings.TrimSpace(authCtx.ActiveOrganizationID) == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "authenticated organization is required")
	}
	if authCtx.ProjectID == nil || *authCtx.ProjectID == uuid.Nil {
		return nil, oops.E(oops.CodeInvalid, nil, "authenticated project is required")
	}

	authCopy := *authCtx
	projectID := *authCtx.ProjectID
	authCopy.ProjectID = &projectID
	payloadCopy := *payload
	payloadCopy.ApikeyToken = nil
	payloadCopy.ProjectSlugInput = nil
	options.SourceAttributes = maps.Clone(options.SourceAttributes)
	options.OutputToolCalls = append([]any(nil), options.OutputToolCalls...)

	ctx = contextvalues.SetAuthContext(ctx, &authCopy)
	ctx = context.WithValue(ctx, authenticatedIngestOptionsKey{}, options)
	return s.ingest(ctx, &payloadCopy)
}

// Ingest is the feature-first hook endpoint; this path only accepts the
// canonical Gram contract. Auth is optional so hook senders stay non-blocking
// for machines that never signed in: a keyless request is acknowledged without
// processing (there is nothing to attribute it to), while a presented key that
// fails validation is a hard 401 — the sender explicitly tried to
// authenticate, and its credential-recovery path keys off that status. Events
// are attributed to the sender's self-reported user email when the payload
// carries one — plugins publish with an org-wide hooks key whose AuthContext
// identity is the publishing admin, not the developer at the keyboard —
// falling back to the token owner for personal keys and senders without a
// device agent.
func (s *Service) Ingest(ctx context.Context, payload *gen.IngestPayload) (res *gen.IngestHookResult, err error) {
	detailed, err := s.ingest(ctx, payload)
	if err != nil {
		return nil, err
	}
	return detailed.Result, nil
}

func (s *Service) ingest(ctx context.Context, payload *gen.IngestPayload) (res *AuthenticatedIngestResult, err error) {
	start := time.Now()
	source := ""
	eventType := ""
	orgSlug := ""
	outcome := hookMetricOutcomeAccepted
	ctx, riskScanned := withRiskScanTracker(ctx)
	defer func() {
		if err != nil && outcome == hookMetricOutcomeAccepted {
			outcome = hookMetricOutcomeFailure
		}
		decision := hookMetricDecisionNone
		if res != nil && res.Result != nil {
			decision = res.Result.Decision
		}
		s.metrics.RecordHookEventDuration(ctx, source, eventType, outcome, decision, orgSlug, *riskScanned, time.Since(start))
	}()

	if err := validateCanonicalIngestPayload(payload); err != nil {
		return nil, err
	}
	source = strings.TrimSpace(payload.Source.Adapter)
	eventType = strings.TrimSpace(payload.Event.Type)
	if apikey := strings.TrimSpace(conv.PtrValOr(payload.ApikeyToken, "")); apikey != "" {
		authedCtx, err := s.authorizePluginRequest(ctx, apikey, strings.TrimSpace(conv.PtrValOr(payload.ProjectSlugInput, "")))
		if err != nil {
			outcome = hookMetricOutcomeUnauthorized
			return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized")
		}
		ctx = authedCtx
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		outcome = hookMetricOutcomeUnauthenticated
		s.logger.InfoContext(ctx, "unauthenticated hook acknowledged without processing",
			attr.SlogEvent("hooks_ingest_unauthenticated"),
			attr.SlogHookSource(source),
			attr.SlogHookEvent(eventType),
		)
		return &AuthenticatedIngestResult{
			Result: canonicalAllowResult(),
			Actor:  ResolvedActor{UserID: "", Email: ""},
		}, nil
	}
	orgSlug = authCtx.OrganizationSlug
	actor := s.resolveCanonicalActor(ctx, payload, authCtx)

	sessionID := canonicalSessionID(payload)
	timestamp := canonicalEventTime(payload)

	replayed := conv.PtrValOr(payload.Replayed, false)

	logger := s.logger.With(
		attr.SlogHookSource(source),
		attr.SlogHookEvent(eventType),
		attr.SlogToolName(canonicalToolName(payload)),
		attr.SlogGenAIConversationID(sessionID),
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogHookReplayed(replayed),
	)
	logger.InfoContext(ctx, "unified hook received", attr.SlogEvent("hooks_ingest"))

	if !s.claimHookIdempotency(ctx, conv.PtrValOr(payload.IdempotencyKey, ""), replayed) {
		ctx = withHookDuplicate(ctx)
	}

	blockReason, userReason := s.evaluateCanonicalHook(ctx, payload, authCtx, actor, timestamp)
	skillCapture, observed, observationErr := s.recordSkillActivation(ctx, payload, authCtx, actor, timestamp, blockReason)
	if observationErr != nil {
		logger.WarnContext(ctx, "failed to record skill activation",
			attr.SlogError(observationErr),
			attr.SlogName(canonicalSkillName(payload)),
		)
		skillCapture = nil
	}
	// A recorded activation puts a new unit in the enqueue queue. Session-end
	// wakes are intentionally omitted: a normal session is not eligible until
	// the quiet window anyway, and waking before detached transcript persistence
	// can score an old visible prefix. Durable message observers and the sweep
	// provide the later wake.
	if observed {
		s.signalSkillEfficacy(ctx, *authCtx.ProjectID)
	}
	mcpInventory := canonicalMCPInventoryEntries(payload)
	if !s.isHookDuplicate(ctx) {
		// Detach from request cancellation: the idempotency token is already
		// claimed, so a client disconnect here would otherwise drop the event
		// for good — the retry gets marked duplicate and skips persistence.
		persistCtx := context.WithoutCancel(ctx)
		s.upsertShadowMCPInventoryURLs(
			persistCtx,
			authCtx.ActiveOrganizationID,
			authCtx.ProjectID.String(),
			canonicalSessionID(payload),
			mcpInventory,
		)
		s.recordCanonicalHook(persistCtx, payload, authCtx, actor, timestamp, blockReason)
	}
	// Cache the inventory and extend its TTL for duplicates too, for the same
	// reason captureMCPAttribution does below: the write is idempotent, and
	// skipping retries would leave a session whose first delivery claimed the
	// idempotency key but failed its cache write with no inventory for its
	// whole life — under block_all every later meta-tool call would then deny,
	// including Gram-hosted targets, with no path to recover.
	s.cacheCanonicalMCPList(
		context.WithoutCancel(ctx),
		canonicalSessionID(payload),
		mcpInventory,
		canonicalMCPInventoryRead(payload),
	)
	// Transcript-derived MCP attribution (Claude Stop/SubagentStop): stash
	// tuples for the scheduled staged-telemetry sweep to join. Runs for
	// duplicate deliveries too — the Redis Set is idempotent, and skipping
	// retries would permanently lose attribution when the first delivery's
	// cache write failed transiently (the retry arrives already marked
	// duplicate).
	s.captureMCPAttribution(context.WithoutCancel(ctx), payload, authCtx)
	if blockReason != "" {
		return &AuthenticatedIngestResult{
			Result: s.withOrgSettings(ctx, authCtx.ActiveOrganizationID, canonicalDenyResult(userReason), skillCapture),
			Actor:  ResolvedActor(actor),
		}, nil
	}
	return &AuthenticatedIngestResult{
		Result: s.withOrgSettings(ctx, authCtx.ActiveOrganizationID, canonicalAllowResult(), skillCapture),
		Actor:  ResolvedActor(actor),
	}, nil
}

type skillCaptureSignal struct {
	rawSHA256       string
	contentRequired bool
}

// recordSkillActivation durably records one skill activation. The second
// return reports whether an observation row was actually written — distinct
// from a nil capture signal, which only says the payload carried no usable raw
// hash — so callers can tell a durable write apart from a no-op or a failure.
func (s *Service) recordSkillActivation(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor, seenAt time.Time, blockReason string) (*skillCaptureSignal, bool, error) {
	if payload.Data == nil || payload.Data.Skill == nil {
		return nil, false, nil
	}
	skill := payload.Data.Skill
	name := canonicalSkillName(payload)
	if name == "" || !isExplicitSkillActivation(payload) && blockReason != "" {
		return nil, false, nil
	}
	writeCtx, cancel := context.WithTimeout(ctx, skillObservationWriteTimeout)
	defer cancel()

	rawSHA256 := normalizeRawSHA256(conv.PtrValOr(skill.RawSha256, ""))
	var capture *skillCaptureSignal
	if rawSHA256 != "" {
		known, err := s.repo.RememberKnownSkillRawHash(writeCtx, repo.RememberKnownSkillRawHashParams{
			ProjectID: *authCtx.ProjectID,
			RawSha256: rawSHA256,
		})
		if err != nil {
			return nil, false, fmt.Errorf("resolve known skill raw hash: %w", err)
		}
		contentRequired := !known
		if known && s.piScanner != nil {
			contentRequired, err = s.repo.SkillRawHashNeedsPromptInjectionScan(writeCtx, repo.SkillRawHashNeedsPromptInjectionScanParams{
				ProjectID: *authCtx.ProjectID,
				RawSha256: rawSHA256,
			})
			if err != nil {
				return nil, false, fmt.Errorf("check known skill scan state: %w", err)
			}
		}
		capture = &skillCaptureSignal{rawSHA256: rawSHA256, contentRequired: contentRequired}
	}
	written, err := s.repo.InsertSkillObservation(writeCtx, repo.InsertSkillObservationParams{
		ProjectID:      *authCtx.ProjectID,
		IdempotencyKey: conv.ToPGTextEmpty(strings.TrimSpace(conv.PtrValOr(payload.IdempotencyKey, ""))),
		Provider:       strings.TrimSpace(payload.Source.Adapter),
		UserID:         conv.ToPGTextEmpty(actor.UserID),
		UserEmail:      conv.ToPGTextEmpty(actor.Email),
		Hostname:       conv.ToPGTextEmpty(strings.TrimSpace(conv.PtrValOr(payload.Source.Hostname, ""))),
		SessionID:      conv.ToPGTextEmpty(canonicalSessionID(payload)),
		SkillName:      name,
		Source:         conv.ToPGTextEmpty(strings.TrimSpace(conv.PtrValOr(skill.Source, ""))),
		SourceLevel:    conv.ToPGTextEmpty(strings.TrimSpace(conv.PtrValOr(skill.SourceLevel, ""))),
		SourcePath:     conv.ToPGTextEmpty(strings.TrimSpace(conv.PtrValOr(skill.SourcePath, ""))),
		RawSha256:      conv.ToPGTextEmpty(rawSHA256),
		SeenAt:         conv.ToPGTimestamptz(seenAt),
	})
	if err != nil {
		return nil, false, fmt.Errorf("insert skill observation: %w", err)
	}
	return capture, written > 0, nil
}

func normalizeRawSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

// withOrgSettings attaches the org-level settings hook senders mirror locally
// so they remain available when the control plane is unreachable. The value is
// carried on every authenticated response — including denies and a `false`
// setting — so a sender's cached copy converges on any successful exchange.
// Best-effort: on lookup failure the effects are omitted and senders keep
// their last-seen value.
func (s *Service) withOrgSettings(ctx context.Context, orgID string, res *gen.IngestHookResult, capture *skillCaptureSignal) *gen.IngestHookResult {
	if s.productFeatures == nil {
		return res
	}
	// Detach from request cancellation: the feature client answers a canceled
	// lookup with (false, nil), which would read as a definitive fail-closed
	// posture rather than an omitted one. Re-bound the detached context — this
	// runs on the blocking verdict path, and a best-effort lookup must not be
	// able to hold an already-computed verdict hostage to a slow feature store
	// (a deadline-less pool acquire can wait unboundedly under saturation).
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	lookup := func(feature productfeatures.Feature) (bool, error) {
		return s.productFeatures.IsFeatureEnabled(lookupCtx, orgID, feature)
	}
	settings := map[string]any{}
	failOpen, failOpenErr := lookup(productfeatures.FeatureHooksFailOpen)
	if failOpenErr != nil {
		s.logger.WarnContext(ctx, "failed to resolve hooks fail-open setting for ingest effects",
			attr.SlogError(failOpenErr),
			attr.SlogOrganizationID(orgID),
		)
	} else {
		settings["fail_open"] = failOpen
	}
	metadataOnly, metadataErr := lookup(productfeatures.FeatureSkillCaptureMetadataOnly)
	if metadataErr != nil {
		s.logger.WarnContext(ctx, "failed to resolve skill capture privacy setting for ingest effects",
			attr.SlogError(metadataErr),
			attr.SlogOrganizationID(orgID),
		)
	} else {
		settings["skill_capture_metadata_only"] = metadataOnly
	}
	if res.Effects == nil {
		res.Effects = map[string]any{}
	}
	if len(settings) > 0 {
		res.Effects["org_settings"] = settings
	}
	if capture != nil && metadataErr == nil && !metadataOnly {
		skillsEnabled, skillsErr := lookup(productfeatures.FeatureSkills)
		if skillsErr != nil {
			s.logger.WarnContext(ctx, "failed to resolve skills entitlement for ingest effects",
				attr.SlogError(skillsErr),
				attr.SlogOrganizationID(orgID),
			)
		} else if skillsEnabled {
			res.Effects["skill_capture"] = map[string]any{
				"raw_sha256":       capture.rawSHA256,
				"content_required": capture.contentRequired,
			}
		}
	}
	if len(res.Effects) == 0 {
		res.Effects = nil
	}
	return res
}

// canonicalActor is the human the event is attributed to. Distinct from the
// AuthContext identity: an org-wide plugin key authenticates many machines,
// so its owner (the publishing admin) must not absorb every developer's
// telemetry.
type canonicalActor struct {
	UserID string
	Email  string
}

// resolveCanonicalActor picks the attribution identity for one ingested event:
// the payload's self-reported user email when present (matching the legacy
// per-provider paths, which always trusted the sender's user_email), otherwise
// the authenticated token owner. Publish-minted plugin keys are shared by the
// whole org, so their owner — the admin who published the plugin — is never
// used as a fallback: an event from such a key with no self-reported email
// stays unattributed rather than crediting every machine to the publisher.
func (s *Service) resolveCanonicalActor(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) canonicalActor {
	tokenEmail := ""
	if authCtx.Email != nil {
		tokenEmail = strings.TrimSpace(*authCtx.Email)
	}
	selfReported := canonicalSourceUserEmail(payload)
	if selfReported == "" {
		if authCtx.OrgWidePluginHooksKey {
			return s.cachedSessionActor(ctx, payload, authCtx)
		}
		return canonicalActor{UserID: authCtx.UserID, Email: tokenEmail}
	}
	if strings.EqualFold(selfReported, tokenEmail) {
		return canonicalActor{UserID: authCtx.UserID, Email: tokenEmail}
	}
	actor := canonicalActor{
		UserID: s.resolveUserByEmail(ctx, selfReported, authCtx.ActiveOrganizationID),
		Email:  selfReported,
	}
	if actor.UserID == "" {
		// A self-reported email that matches no Gram user cannot key
		// user-scoped policies; recover a complete identity instead of
		// running unattributed. For shared plugin keys the session metadata
		// cache may already link this session to a user (an earlier canonical
		// SessionStart hook, the OTEL path, or the device bridge). A personal
		// key already identifies the developer, so their events keep the owner
		// identity, exactly as when no email is
		// self-reported. Either way policy enforcement and the recorded rows
		// stay on one identity.
		if authCtx.OrgWidePluginHooksKey {
			if cached := s.cachedSessionActor(ctx, payload, authCtx); cached.UserID != "" {
				return cached
			}
		} else if authCtx.UserID != "" {
			return canonicalActor{UserID: authCtx.UserID, Email: tokenEmail}
		}
	}
	return actor
}

// cachedSessionActor recovers attribution for a shared plugin-key event with
// no self-reported email from the session metadata cache (seeded by the OTEL
// path or the device bridge). Resolving here — not just at persistence time in
// canonicalSessionMetadata — keeps policy enforcement and the recorded rows on
// the same identity: user-scoped shadow-MCP policies must see the user the
// session is already attributed to. Only an entry seeded by the same
// org+project is trusted (the cache is keyed by session id alone).
func (s *Service) cachedSessionActor(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) canonicalActor {
	sessionID := canonicalSessionID(payload)
	if sessionID == "" {
		return canonicalActor{UserID: "", Email: ""}
	}
	cached, err := s.getSessionMetadata(ctx, sessionID)
	if err != nil ||
		cached.GramOrgID != authCtx.ActiveOrganizationID ||
		cached.ProjectID != authCtx.ProjectID.String() {
		return canonicalActor{UserID: "", Email: ""}
	}
	return canonicalActor{UserID: cached.UserID, Email: cached.UserEmail}
}

func canonicalSourceUserEmail(payload *gen.IngestPayload) string {
	if payload != nil && payload.Source != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Source.UserEmail, ""))
	}
	return ""
}

func validateCanonicalIngestPayload(payload *gen.IngestPayload) error {
	if payload == nil || payload.Source == nil || payload.Event == nil {
		return oops.E(oops.CodeInvalid, nil, "source and event are required")
	}
	adapter := strings.TrimSpace(payload.Source.Adapter)
	if adapter == "" {
		return oops.E(oops.CodeInvalid, nil, "source.adapter is required")
	}
	// The assistant surface is derived from the observation provider, and only
	// server-side assistant runs may claim it. Reserve those values so a client
	// hook cannot forge assistant-attributed skill activity.
	if isReservedAssistantAdapter(adapter) {
		return oops.E(oops.CodeInvalid, nil, "source.adapter is reserved")
	}
	if strings.TrimSpace(payload.Event.Type) == "" {
		return oops.E(oops.CodeInvalid, nil, "event.type is required")
	}
	if strings.TrimSpace(payload.SchemaVersion) != hookIngestSchemaV1 {
		return oops.E(oops.CodeInvalid, nil, "unsupported hook schema_version")
	}
	return nil
}

func isReservedAssistantAdapter(adapter string) bool {
	switch strings.ToLower(strings.Join(strings.Fields(adapter), "")) {
	case "assistant", "assistants":
		return true
	default:
		return false
	}
}

func (s *Service) evaluateCanonicalHook(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor, timestamp time.Time) (string, string) {
	event := canonicalHookEvent(payload, authCtx, actor, timestamp)
	eventType := strings.TrimSpace(payload.Event.Type)

	// Spend gate runs before any risk-policy evaluation, for every adapter
	// with a per-provider enforcement surface (claude, codex, cursor) — the
	// risk scans below already run adapter-agnostically, and an over-budget
	// actor is over budget regardless of which agent carries the event.
	// Adapters are self-reported slugs, so this remains a cooperative-client
	// boundary like the rest of the ingest surface; matching is on the
	// lowercased value so a case variant cannot dodge the gate. opencode
	// still passes through untouched pending a product decision on its
	// enforcement surface.
	if spendGatedAdapter(payload.Source.Adapter) && (eventType == "prompt.submitted" || eventType == "tool.requested") {
		if block := s.checkSpendGate(ctx, event); block != nil {
			if eventType == "tool.requested" {
				kind := "tool call"
				if canonicalPermissionType(payload) != "" {
					// Permission-shaped tool.requested events keep the
					// permission framing, matching this path's risk wording
					// and the legacy codex endpoint's spend deny.
					kind = "permission request"
				}
				auditReason := spendBlockReason(kind, block)
				return auditReason, s.appendCanonicalBlockURL(ctx, authCtx, actor, payload, auditReason, canonicalToolName(payload), "", auditReason)
			}
			auditReason := spendBlockReason("prompt", block)
			return auditReason, auditReason
		}
	}

	switch eventType {
	case "prompt.submitted":
		ev := hookevents.NewUserPromptSubmit(event, hookevents.UserPromptSubmitParams{
			Prompt: canonicalPromptText(payload),
		})
		if scanResult := s.scanUserPromptForEnforcement(ctx, ev); scanResult != nil {
			if scanResult.Action == "warn" && authenticatedIngestOptions(ctx).AllowWarnAcknowledgement {
				if s.warnAcknowledged(ctx, ev.Event, scanResult, "") {
					return "", ""
				}
				if _, userReason, ok := s.warnDenyReason(ctx, ev.Event, scanResult, ""); ok {
					auditReason := fmt.Sprintf("Speakeasy challenged this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
					return auditReason, userReason
				}
			}
			auditReason := fmt.Sprintf("Speakeasy blocked this prompt: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			return auditReason, renderUserBlockReason(scanResult.UserMessage, auditReason)
		}
	case "tool.requested":
		toolName := canonicalToolName(payload)
		toolInput := canonicalToolInput(payload)
		if permissionType := canonicalPermissionType(payload); permissionType != "" {
			ev := hookevents.NewPermissionRequest(event, hookevents.PermissionRequestParams{
				ToolName:       toolName,
				ToolInput:      toolInput,
				PermissionType: permissionType,
			})
			// An acknowledged permission warn clears only this risk challenge; it
			// must still fall through to the MCP/shadow-MCP guard below, never
			// short-circuit the tool call (mirrors the Claude PreToolUse handler).
			// So exclude acknowledged warns from the block condition rather than
			// returning early on them.
			if scanResult := s.scanPermissionRequestForEnforcement(ctx, ev); scanResult != nil &&
				(scanResult.Action != "warn" || !s.warnAcknowledged(ctx, ev.Event, scanResult, toolName)) {
				if scanResult.Action == "warn" {
					if _, userReason, ok := s.warnDenyReason(ctx, ev.Event, scanResult, toolName); ok {
						auditReason := fmt.Sprintf("Speakeasy challenged this permission request: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
						return auditReason, userReason
					}
				}
				auditReason := fmt.Sprintf("Speakeasy blocked this permission request: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
				userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
				return auditReason, s.appendCanonicalBlockURL(ctx, authCtx, actor, payload, auditReason, toolName, scanResult.PolicyID, userReason)
			}
		}
		if canonicalMCPData(payload) != nil || toolref.IsMCPToolName(toolName) || s.canonicalCodexMetaTool(ctx, payload, toolName, toolInput) {
			ev := hookevents.NewBeforeMCPExecution(event, hookevents.BeforeMCPExecutionParams{
				ToolName:  toolName,
				ToolInput: toolInput,
			})
			if scanResult := s.scanMCPRequestForEnforcement(ctx, ev); scanResult != nil {
				if scanResult.Action == "warn" {
					if s.warnAcknowledged(ctx, ev.Event, scanResult, toolName) {
						return s.evaluateCanonicalShadowMCP(ctx, authCtx, actor, payload, toolName, toolInput)
					}
					if _, userReason, ok := s.warnDenyReason(ctx, ev.Event, scanResult, toolName); ok {
						auditReason := fmt.Sprintf("Speakeasy challenged this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
						return auditReason, userReason
					}
				}
				auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
				userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
				return auditReason, s.appendCanonicalBlockURL(ctx, authCtx, actor, payload, auditReason, toolName, scanResult.PolicyID, userReason)
			}
			return s.evaluateCanonicalShadowMCP(ctx, authCtx, actor, payload, toolName, toolInput)
		}
		ev := hookevents.NewBeforeToolUse(event, hookevents.BeforeToolUseParams{
			ToolName:  toolName,
			ToolInput: toolInput,
		})
		if scanResult := s.scanToolRequestForEnforcement(ctx, ev); scanResult != nil {
			if scanResult.Action == "warn" {
				if s.warnAcknowledged(ctx, ev.Event, scanResult, toolName) {
					return "", ""
				}
				if _, userReason, ok := s.warnDenyReason(ctx, ev.Event, scanResult, toolName); ok {
					auditReason := fmt.Sprintf("Speakeasy challenged this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
					return auditReason, userReason
				}
			}
			auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", scanResult.PolicyName, scanResult.Description)
			userReason := renderUserBlockReason(scanResult.UserMessage, auditReason)
			return auditReason, s.appendCanonicalBlockURL(ctx, authCtx, actor, payload, auditReason, toolName, scanResult.PolicyID, userReason)
		}
	}
	return "", ""
}

// appendCanonicalBlockURL mints the durable block row for a policy-denied
// tool call and attaches its URL to the agent-facing reason, matching the
// legacy per-provider handlers. Retried deliveries keep the deny but must not
// mint a second row.
func (s *Service) appendCanonicalBlockURL(ctx context.Context, authCtx *contextvalues.AuthContext, actor canonicalActor, payload *gen.IngestPayload, auditReason, toolName, policyID, userReason string) string {
	if s.isHookDuplicate(ctx) {
		return userReason
	}
	bURL := s.recordToolCallBlockAsync(ctx, toolCallBlockParams{
		Provider:       strings.TrimSpace(payload.Source.Adapter),
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Reason:         auditReason,
		ToolName:       toolName,
		UserID:         actor.UserID,
		RiskPolicyID:   conv.StringToNullUUID(policyID),
		RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ChatID:         chatIDForBlock(canonicalSessionID(payload)),
		ChatMessageID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if bURL == "" {
		return userReason
	}
	return appendBlockURL(userReason, bURL)
}

func canonicalHookEvent(payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor, timestamp time.Time) hookevents.Event {
	rawEvent := strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, ""))
	if rawEvent == "" {
		rawEvent = strings.TrimSpace(payload.Event.Type)
	}
	return hookevents.Event{
		Provider:     hookevents.Provider(strings.TrimSpace(payload.Source.Adapter)),
		Type:         canonicalRiskEventType(payload),
		RawEventType: rawEvent,
		Timestamp:    timestamp,
		AuthContext:  authCtx,
		Context: hookevents.EventContext{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			User: hookevents.User{
				ID:    actor.UserID,
				Email: actor.Email,
			},
		},
		ConversationID: canonicalSessionID(payload),
		Raw:            payload,
	}
}

func canonicalRiskEventType(payload *gen.IngestPayload) hookevents.EventType {
	switch strings.TrimSpace(payload.Event.Type) {
	case "prompt.submitted":
		return hookevents.EventTypeUserPromptSubmit
	case "tool.requested":
		if canonicalMCPData(payload) != nil || toolref.IsMCPToolName(canonicalToolName(payload)) {
			return hookevents.EventTypeBeforeMCPExecution
		}
		if canonicalPermissionType(payload) != "" {
			return hookevents.EventTypePermissionRequest
		}
		return hookevents.EventTypeBeforeToolUse
	case "tool.completed":
		if canonicalMCPData(payload) != nil {
			return hookevents.EventTypeAfterMCPExecution
		}
		return hookevents.EventTypeAfterToolUse
	case "tool.failed":
		return hookevents.EventTypeAfterToolUseFailure
	case "assistant.responded":
		return hookevents.EventTypeAfterAgentResponse
	case "assistant.thought":
		return hookevents.EventTypeAfterAgentThought
	case "session.started":
		return hookevents.EventTypeSessionStart
	case "session.updated":
		return hookevents.EventTypeConfigChange
	case "session.ended":
		return hookevents.EventTypeSessionEnd
	case "notification.reported":
		return hookevents.EventTypeNotification
	default:
		return hookevents.EventType(strings.TrimSpace(payload.Event.Type))
	}
}

func (s *Service) evaluateCanonicalShadowMCP(ctx context.Context, authCtx *contextvalues.AuthContext, actor canonicalActor, payload *gen.IngestPayload, rawToolName string, toolInput any) (string, string) {
	policy := s.lookupShadowMCPBlockingPolicy(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), actor.UserID)
	if policy == nil {
		return "", ""
	}

	toolName := toolref.MCPFunctionOf(rawToolName)
	evidence := canonicalShadowMCPEvidence(payload, rawToolName)
	// A Codex meta-tool names its target in tool_input.server, so nothing above
	// can derive an identity from the tool name. Resolving that name against the
	// session's inventory is what lets a Gram-hosted target be allowed at all —
	// without a URL the guard can only reach its generic "not Gram-hosted" deny,
	// which would block legitimate reads the legacy endpoint permits. A name we
	// cannot resolve still denies: unproven is not absent.
	if evidence.ServerIdentity == "" && evidence.FullURL == "" {
		if server, isMetaTool := codexMetaToolServer(rawToolName, toolInput); isMetaTool {
			evidence.ServerIdentity = server
			s.resolveEvidenceFromSessionInventory(ctx, &evidence, canonicalSessionID(payload))
		}
	}
	if detail, denied := s.enforceShadowMCPToolAccess(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), actor.UserID, policy, toolName, evidence); denied {
		auditReason := fmt.Sprintf("Speakeasy blocked this tool call: matched policy %q (%s)", policy.Name, detail)
		userReason := s.renderShadowMCPUserBlockReason(ctx, shadowMCPRequestLinkParams{
			OrganizationID:  authCtx.ActiveOrganizationID,
			ProjectID:       authCtx.ProjectID.String(),
			RequesterUserID: actor.UserID,
			UserMessage:     policy.UserMessage,
			AuditReason:     auditReason,
			Evidence:        evidence,
			ToolName:        toolName,
			ToolInput:       toolInput,
			RiskPolicyID:    policy.ID,
		})
		// Retried deliveries still get the deny decision, but must not mint
		// another block row (and a second block URL) for the same call.
		if !s.isHookDuplicate(ctx) {
			if bURL := s.recordToolCallBlockAsync(ctx, toolCallBlockParams{
				Provider:       strings.TrimSpace(payload.Source.Adapter),
				OrganizationID: authCtx.ActiveOrganizationID,
				ProjectID:      *authCtx.ProjectID,
				Reason:         auditReason,
				ToolName:       toolName,
				UserID:         actor.UserID,
				RiskPolicyID:   conv.StringToNullUUID(policy.ID),
				RiskResultID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				// Deliberately unlinked. chat_id carries an FK to chats, and a
				// shadow-MCP deny can land before the session's chat row is
				// persisted — passing chatIDForBlock here violates the FK and
				// the whole block insert is lost, taking the block URL with it.
				// Linking needs the chat row guaranteed first (DNO-767).
				ChatID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
				ChatMessageID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			}); bURL != "" {
				userReason = appendBlockURL(userReason, bURL)
			}
		}
		return auditReason, userReason
	}
	return "", ""
}

// resolveEvidenceFromSessionInventory upgrades evidence carrying only a server
// name to the target that name resolves to in the session's inventory: the URL
// for HTTP servers, the launch command for stdio ones. Mirrors the legacy
// codexShadowMCPEvidence resolution — stdio identity is pinned to the command
// so a bypass grant cannot follow a renamed config alias.
func (s *Service) resolveEvidenceFromSessionInventory(ctx context.Context, evidence *shadowmcp.AccessEvidence, sessionID string) {
	if evidence.ServerIdentity == "" || sessionID == "" {
		return
	}
	entries, err := s.getCachedMCPList(ctx, sessionID)
	if err != nil {
		return
	}
	applyMCPEntryToEvidence(evidence, matchCodexCachedMCPServerEntry(entries, evidence.ServerIdentity))
}

// cacheCanonicalMCPList stores a session's MCP inventory under the same key
// and TTL the legacy per-provider endpoints use, so the shadow-MCP guard can
// resolve a later tool call's target to a configured server. Best-effort: a
// cache miss downgrades a deny's detail, it never changes the decision.
func (s *Service) cacheCanonicalMCPList(ctx context.Context, sessionID string, entries []MCPServerEntry, inventoryRead bool) {
	if sessionID == "" {
		return
	}

	// Extend both keys on every event, as the legacy endpoints do for the
	// snapshot: a session outliving its TTL loses the inventory, and losing the
	// read status silently disables the guard for the rest of that session.
	s.refreshMCPListTTL(ctx, sessionID)
	if err := s.cache.Expire(ctx, sessionMCPInventoryReadCacheKey(sessionID), sessionMCPInventoryReadTTL); err != nil {
		s.logger.DebugContext(ctx, "failed to extend MCP inventory read status",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
		)
	}

	// Only a successful inventory read is authoritative, even when the
	// inventory is empty or omitted. Partial reads only refresh an existing
	// snapshot so they cannot downgrade complete evidence.
	// Write the entries before the read status. The status is what licenses the
	// guard to treat an empty inventory as proof no servers exist, so recording
	// it while the entries write failed would leave the session claiming a read
	// it cannot back up — and under block_all every later meta-tool call denies
	// for the rest of the session.
	if !inventoryRead {
		return
	}
	if err := s.cache.Set(ctx, sessionMCPListCacheKey(sessionID), entries, sessionMCPListTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to cache MCP list snapshot",
			attr.SlogEvent("hook_mcp_list_cache_set_failed"),
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
		)
		return
	}

	// Meta-tool calls arrive later carrying no inventory status, so the
	// authoritative read status has to be held per session.
	if err := s.cache.Set(ctx, sessionMCPInventoryReadCacheKey(sessionID), true, sessionMCPInventoryReadTTL); err != nil {
		s.logger.WarnContext(ctx, "failed to cache MCP inventory read status",
			attr.SlogEvent("hook_mcp_list_read_cache_set_failed"),
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
		)
	}
}

// canonicalCodexMetaTool reports whether this event is one of Codex's built-in
// MCP resource tools. They carry no mcp__ prefix and agenthooks resolves MCP
// data by that same prefix, so without this check they reach neither arm of the
// gate and a shadow-MCP policy never sees them (DNO-767). Scoped to the codex
// adapter: another agent's unrelated tool of the same name is not an MCP call.
func (s *Service) canonicalCodexMetaTool(ctx context.Context, payload *gen.IngestPayload, toolName string, toolInput any) bool {
	if payload == nil || !strings.EqualFold(strings.TrimSpace(payload.Source.Adapter), "codex") {
		return false
	}
	_, isMetaTool := codexMetaToolServer(toolName, toolInput)
	if !isMetaTool {
		return false
	}
	if !s.canonicalClientReportsMCPInventory(ctx, payload) {
		// Counts the un-upgraded population: once this stops firing, the
		// capability check can go and the guard can apply unconditionally.
		s.logger.InfoContext(ctx, "skipping codex meta-tool shadow-mcp guard: client cannot report MCP inventory",
			attr.SlogEvent("shadow_mcp_meta_tool_client_incapable"),
			attr.SlogToolName(toolName),
		)
		return false
	}
	return true
}

// canonicalClientReportsMCPInventory reports whether this session's MCP server
// list was actually read, which is what makes an empty inventory mean anything.
//
// The guard denies a meta-tool call it cannot clear against an inventory, so it
// has to tell "the agent has no MCP servers" from "we could not read the list".
// Both arrive as zero entries. Only the sender knows which happened, and it
// says so with mcp_inventory_collected — true means the list was read (an empty
// one then genuinely means no servers, and denying is right), false or absent
// means it could not be, so the inventory proves nothing and the guard must not
// treat it as proof of absence.
//
// Absent also covers every relay released before the flag existed. Those send
// no inventory at all, and enforcing on them would deny every meta-tool call
// including reads of Gram-hosted servers that work today — so they keep their
// current behavior until they upgrade, rather than enforcement depending on a
// server deploy and a hooks release landing in the right order.
func (s *Service) canonicalClientReportsMCPInventory(ctx context.Context, payload *gen.IngestPayload) bool {
	if canonicalMCPInventoryRead(payload) {
		return true
	}
	sessionID := canonicalSessionID(payload)
	if sessionID == "" {
		return false
	}
	var read bool
	if err := s.cache.Get(ctx, sessionMCPInventoryReadCacheKey(sessionID), &read); err != nil {
		return false
	}
	return read
}

// canonicalMCPInventoryRead reports the sender's own claim that this event
// carries a complete inventory.
func canonicalMCPInventoryRead(payload *gen.IngestPayload) bool {
	return payload != nil && payload.Data != nil && conv.PtrValOr(payload.Data.McpInventoryCollected, false)
}

func canonicalShadowMCPEvidence(payload *gen.IngestPayload, rawToolName string) shadowmcp.AccessEvidence {
	mcp := canonicalMCPData(payload)
	if mcp == nil {
		return shadowmcp.AccessEvidence{
			FullURL:        "",
			URLHost:        "",
			ServerIdentity: toolref.MCPServerOf(rawToolName),
		}
	}
	identity := strings.TrimSpace(conv.PtrValOr(mcp.ServerIdentity, ""))
	if identity == "" {
		identity = strings.TrimSpace(conv.PtrValOr(mcp.Command, ""))
	}
	if identity == "" {
		identity = strings.TrimSpace(conv.PtrValOr(mcp.ServerName, ""))
	}
	if identity == "" {
		identity = toolref.MCPServerOf(rawToolName)
	}
	return shadowmcp.AccessEvidence{
		FullURL:        strings.TrimSpace(conv.PtrValOr(mcp.URL, "")),
		URLHost:        "",
		ServerIdentity: identity,
	}
}

func (s *Service) recordCanonicalHook(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor, timestamp time.Time, blockReason string) {
	// Resolve the session identity once, before the telemetry write, so the
	// hook row and the chat persistence below stamp the same AI-account
	// attribution.
	metadata := s.canonicalSessionMetadata(ctx, payload, authCtx, actor)
	// Resolve the product surface once per event: the OTEL-cached service.name
	// wins ("cowork" vs "claude-code"), the SessionStart variant fills in for
	// sessions whose OTEL stream hasn't arrived, and non-Claude adapters pass
	// through unchanged. Telemetry hook_source and the chat message source both
	// stamp this value.
	hookSource := conv.Default(s.claudeSessionSurface(ctx, &metadata), strings.TrimSpace(payload.Source.Adapter))
	// Hostname counts as cacheable identity: a session ingested with an
	// org-scoped key and no self-reported email carries nothing else, and the
	// OTEL path needs the cached hostname to stamp Claude cost rows so the
	// user breakdown can fall back to the device.
	if strings.TrimSpace(payload.Event.Type) == "session.started" &&
		metadata.SessionID != "" && (metadata.UserID != "" || metadata.UserEmail != "" || metadata.Hostname != "") {
		cacheCtx, cancel := context.WithTimeout(ctx, canonicalSessionCacheWriteTimeout)
		err := s.cache.Set(cacheCtx, sessionCacheKey(metadata.SessionID), metadata, 24*time.Hour)
		cancel()
		if err != nil {
			s.logger.WarnContext(ctx, "failed to cache canonical hook session identity",
				attr.SlogEvent("hooks_ingest_session_cache_failed"),
				attr.SlogError(err),
				attr.SlogGenAIConversationID(metadata.SessionID),
				attr.SlogOrganizationID(metadata.GramOrgID),
				attr.SlogProjectID(metadata.ProjectID),
			)
		}
	}
	s.writeCanonicalTelemetry(ctx, payload, authCtx, &metadata, hookSource, timestamp, blockReason)
	promptCaptured, err := s.persistCanonicalConversationEvent(ctx, payload, authCtx, &metadata, hookSource, timestamp)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to persist canonical hook conversation event",
			attr.SlogEvent("hooks_ingest_chat_persist_failed"),
			attr.SlogError(err),
			attr.SlogHookSource(payload.Source.Adapter),
			attr.SlogHookEvent(payload.Event.Type),
			attr.SlogGenAIConversationID(canonicalSessionID(payload)),
			attr.SlogProjectID(authCtx.ProjectID.String()),
		)
	} else if promptCaptured && usesNativeTranscriptFallback(payload.Source.Adapter) {
		s.markNativePromptSession(ctx, authCtx.ProjectID.String(), canonicalSessionID(payload), payload.Source.Adapter)
	}
	if err := s.persistPromptAttachments(ctx, payload, authCtx, &metadata, timestamp); err != nil {
		s.logger.WarnContext(ctx, "failed to persist prompt attachments",
			attr.SlogEvent("hooks_ingest_prompt_attachment_persist_failed"),
			attr.SlogError(err),
			attr.SlogHookSource(payload.Source.Adapter),
			attr.SlogHookEvent(payload.Event.Type),
			attr.SlogGenAIConversationID(canonicalSessionID(payload)),
			attr.SlogProjectID(authCtx.ProjectID.String()),
		)
	}
}

// canonicalSessionMetadata builds the session identity for a canonical hook
// event: the resolved actor (self-reported user email when the payload
// carries one, else the token owner), enriched with the AI-account
// attribution the OTEL path cached for the session (user_accounts link,
// account_type, provider identity, device-bridge owner). Canonical payloads
// carry no account identity of their own, so without the cached attribution
// telemetry rows and chats captured here reflect only the resolved actor —
// invisible to the account-identity risk rules and the personal/team
// classification. The AI account's own email rides separately in
// ObservedUserEmail (the gram.account_email attribute). The session cache is
// keyed by session id alone, so only trust an entry the same org+project
// seeded.
func (s *Service) canonicalSessionMetadata(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, actor canonicalActor) SessionMetadata {
	metadata := SessionMetadata{
		SessionID:           canonicalSessionID(payload),
		ServiceName:         strings.TrimSpace(payload.Source.Adapter),
		UserEmail:           actor.Email,
		UserID:              actor.UserID,
		Provider:            "",
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
		Hostname:            strings.TrimSpace(conv.PtrValOr(payload.Source.Hostname, "")),
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		ObservedUserEmail:   "",
		GramOrgID:           authCtx.ActiveOrganizationID,
		ProjectID:           authCtx.ProjectID.String(),
	}
	if metadata.SessionID == "" {
		return metadata
	}

	if cached, err := s.getSessionMetadata(ctx, metadata.SessionID); err == nil &&
		cached.GramOrgID == metadata.GramOrgID && cached.ProjectID == metadata.ProjectID {
		// Surface-specificity merge: the OTEL path caches "cowork" from the
		// resource service.name, which must survive this event's re-cache —
		// cowork ships the same "claude-code-desktop" adapter slug as Claude
		// Code Desktop, so the adapter alone can never downgrade it. See
		// claudeServiceNameSpecificity for the full ranking.
		metadata.ServiceName = preferClaudeServiceName(metadata.ServiceName, cached.ServiceName)
		metadata.Provider = cached.Provider
		metadata.ExternalOrgID = cached.ExternalOrgID
		metadata.ExternalAccountUUID = cached.ExternalAccountUUID
		metadata.ExternalAccountID = cached.ExternalAccountID
		metadata.DeviceID = cached.DeviceID
		metadata.Hostname = conv.Default(metadata.Hostname, cached.Hostname)
		metadata.AccountType = cached.AccountType
		metadata.BillingMode = cached.BillingMode
		metadata.UserAccountID = cached.UserAccountID
		// The OTEL path's UserEmail is the account's own report; fall back to it
		// for cache entries written before ObservedUserEmail existed.
		metadata.ObservedUserEmail = conv.Default(cached.ObservedUserEmail, cached.UserEmail)
		// Fill identity only when the resolved actor carried none (org-scoped
		// ingest keys with no self-reported email): the device bridge may have
		// attributed the owning employee. A resolved identity is never
		// overwritten.
		if authenticatedIngestOptions(ctx).AllowSessionIdentityFallback {
			if metadata.UserEmail == "" {
				metadata.UserEmail = cached.UserEmail
			}
			if metadata.UserID == "" {
				metadata.UserID = cached.UserID
			}
		}
	}

	// Codex sessions delivered only through the relay never pass the legacy
	// hook or OTEL paths that normally attribute the account, so classify here
	// when no cached attribution exists or when this event's actor email is
	// not the one the cached classification was computed from (the same
	// identity rule the legacy-hook and OTEL paths apply). A fresh result is
	// written back so later events on any path adopt it instead of
	// re-classifying. Claude adapters are untouched: their attribution belongs
	// to the OTEL path, which carries the account identity this payload lacks.
	if strings.EqualFold(strings.TrimSpace(payload.Source.Adapter), "codex") {
		metadata.Provider = providerOpenAI
		identityChanged := !sameCodexIdentity(metadata.ObservedUserEmail, metadata.UserEmail)
		if metadata.AccountType == "" || identityChanged {
			if identityChanged {
				// The identity fallback above fills UserID from the cache
				// independently of the email, so on an identity change it can
				// still hold the PRIOR actor's resolved id — and
				// classifyAccount reads UserID as the resolution of the email
				// being classified, which would label a new unresolved email
				// team (and unlock the team-gated billing mode) off the old
				// actor. Restore the resolved actor's own id: an identity
				// change means UserEmail is the actor's email, whose
				// resolution resolveCanonicalActor already computed.
				metadata.UserID = actor.UserID
			}
			metadata.ObservedUserEmail = metadata.UserEmail
			if err := s.attributeSession(ctx, &metadata); err != nil {
				s.logger.WarnContext(ctx, "failed to attribute AI account for Codex session",
					attr.SlogEvent("account_attribution_failed"),
					attr.SlogError(err),
					attr.SlogGenAIConversationID(metadata.SessionID),
				)
				// Leave the session unclassified rather than half-attributed:
				// attributeSession stamps AccountType before the step that
				// failed, and both this branch's gate and recordCanonicalHook's
				// session.started cache write key on AccountType alone —
				// keeping the half state would freeze an empty billing mode
				// for the cache lifetime.
				metadata.AccountType = ""
				metadata.BillingMode = ""
			} else if metadata.SessionID != "" && metadata.AccountType != "" &&
				!strings.EqualFold(strings.TrimSpace(payload.Event.Type), "session.started") {
				// session.started is excluded: recordCanonicalHook already
				// persists this metadata (attribution included) for that
				// event; this write-back exists for sessions whose started
				// event was never seen.
				cacheCtx, cancel := context.WithTimeout(ctx, canonicalSessionCacheWriteTimeout)
				err := s.cache.Set(cacheCtx, sessionCacheKey(metadata.SessionID), metadata, 24*time.Hour)
				cancel()
				if err != nil {
					s.logger.WarnContext(ctx, "failed to cache Codex session metadata",
						attr.SlogError(err),
						attr.SlogGenAIConversationID(metadata.SessionID),
					)
				}
			}
		}
	}
	return metadata
}

func (s *Service) writeCanonicalTelemetry(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, metadata *SessionMetadata, hookSource string, timestamp time.Time, blockReason string) {
	if s.telemetryLogger == nil {
		return
	}

	hookEventName := telemetryHookEventName(payload)
	toolName := canonicalTelemetryToolName(payload)
	if toolName == "" {
		toolName = hookEventName
	}

	attrs := hookTelemetryBaseAttrs(payload, authCtx, hookEventName, hookSource)
	if blockReason != "" {
		attrs[attr.HookBlockReasonKey] = blockReason
	}
	if toolName != "" {
		attrs[attr.ToolNameKey] = toolName
	}
	if toolCallID := canonicalToolCallID(payload); toolCallID != "" {
		attrs[attr.GenAIToolCallIDKey] = toolCallID
	}
	if input := canonicalToolInput(payload); input != nil {
		attrs[attr.GenAIToolCallArgumentsKey] = jsonString(input)
	}
	if output := canonicalToolOutput(payload); output != nil {
		attrs[attr.GenAIToolCallResultKey] = jsonString(output)
	}
	if errPayload := canonicalToolError(payload); errPayload != nil {
		attrs[attr.HookErrorKey] = jsonString(errPayload)
	}
	if isInterrupt := canonicalIsInterrupt(payload); isInterrupt != nil {
		attrs[attr.HookIsInterruptKey] = *isInterrupt
	}
	if tool := canonicalToolCallData(payload); tool != nil && tool.DurationMs != nil {
		attrs[attr.ToolCallDurationKey] = time.Duration(*tool.DurationMs * float64(time.Millisecond)).Seconds()
	}
	if usage := canonicalUsageData(payload); usage != nil {
		if usage.InputTokens != nil {
			attrs[attr.GenAIUsageInputTokensKey] = *usage.InputTokens
		}
		if usage.OutputTokens != nil {
			attrs[attr.GenAIUsageOutputTokensKey] = *usage.OutputTokens
		}
		if usage.CacheReadTokens != nil {
			attrs[attr.GenAIUsageCacheReadInputTokensKey] = *usage.CacheReadTokens
		}
		if usage.CacheWriteTokens != nil {
			attrs[attr.GenAIUsageCacheCreationInputTokensKey] = *usage.CacheWriteTokens
		}
		if usage.Cost != nil {
			attrs[attr.GenAIUsageCostKey] = *usage.Cost
		}
	}
	if mcp := canonicalMCPData(payload); mcp != nil {
		if server := strings.TrimSpace(conv.PtrValOr(mcp.ServerIdentity, "")); server != "" {
			attrs[attr.ToolCallSourceKey] = server
		} else if server := strings.TrimSpace(conv.PtrValOr(mcp.ServerName, "")); server != "" {
			attrs[attr.ToolCallSourceKey] = server
		}
		if url := strings.TrimSpace(conv.PtrValOr(mcp.URL, "")); url != "" {
			attrs[attr.MCPServerURLKey] = url
			attrs[attr.MCPMatchKey] = url
		} else if command := strings.TrimSpace(conv.PtrValOr(mcp.Command, "")); command != "" {
			attrs[attr.MCPMatchKey] = command
		}
	}
	skill := canonicalSkillName(payload)
	if skill != "" && isExplicitSkillActivation(payload) {
		attrs[attr.GenAIToolCallArgumentsKey] = jsonString(map[string]string{"skill": skill})
	}
	mergeSourceAttributes(attrs, authenticatedIngestOptions(ctx).SourceAttributes)

	// Carry the account attribution (provider, external_org_id, account_type,
	// device_id) onto every hook event row so per-tool-call telemetry can be
	// split by personal vs team account, matching the legacy per-provider paths.
	stampAccountAttribution(attrs, *metadata)

	s.logHookTelemetry(ctx, authCtx, metadata, timestamp, toolName, attrs)

	// A skill name on an ordinary tool/prompt event is an inferred activation
	// (Codex has no dedicated Skill tool): the underlying event was recorded
	// truthfully above, and the activation gets its own derived row so skill
	// dashboards see the same skill.activated vocabulary as Claude senders.
	// A policy-blocked event never ran, so it is not an activation.
	if skill != "" && !isExplicitSkillActivation(payload) && blockReason == "" {
		attrs = hookTelemetryBaseAttrs(payload, authCtx, eventTypeSkillActivated, hookSource)
		// Skill counts aggregate at trace level (trace_summaries), and its MV
		// resolves tool_name/skill_name with any(): sharing a trace with the
		// underlying tool or prompt rows lets a non-Skill sibling win the
		// summary and drop the activation from skill analytics — and the
		// session-hash fallback would additionally collapse every
		// prompt-mention activation in a session into one summary row. Every
		// derived row gets its own trace.
		attrs[attr.TraceIDKey] = generateTraceID()
		attrs[attr.ToolNameKey] = "Skill"
		attrs[attr.GenAIToolCallArgumentsKey] = jsonString(map[string]string{"skill": skill})
		mergeSourceAttributes(attrs, authenticatedIngestOptions(ctx).SourceAttributes)
		stampAccountAttribution(attrs, *metadata)
		s.logHookTelemetry(ctx, authCtx, metadata, timestamp, "Skill", attrs)
	}
}

func mergeSourceAttributes(base, source map[attr.Key]any) {
	for key, value := range source {
		if _, exists := base[key]; !exists {
			base[key] = value
		}
	}
}

// hookTelemetryBaseAttrs builds the attributes shared by every telemetry row
// derived from one ingested hook event. Each row gets its own span id; the
// trace id is payload-derived so sibling rows stay on one trace.
func hookTelemetryBaseAttrs(payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, hookEventName string, hookSource string) map[attr.Key]any {
	attrs := map[attr.Key]any{
		attr.EventSourceKey:    string(telemetry.EventSourceHook),
		attr.HookEventKey:      hookEventName,
		attr.HookSourceKey:     hookSource,
		attr.ProjectIDKey:      authCtx.ProjectID.String(),
		attr.OrganizationIDKey: authCtx.ActiveOrganizationID,
		attr.SpanIDKey:         generateSpanID(),
		attr.TraceIDKey:        canonicalTraceID(payload),
		attr.LogBodyKey:        "Hook: " + hookEventName,
	}
	// Stamp the resolved chat id, not the raw agent session id: every consumer
	// treats gen_ai.conversation.id (materialized as telemetry_logs.chat_id) as
	// the chats row id, and persistCanonicalConversationEvent stores the
	// transcript under the same mapping. Claude/Codex/Cursor session ids are
	// themselves UUIDs, so this was previously an accidental identity; opencode
	// ids ("ses_...") are not, and the raw string reached the chat detail
	// endpoint as a malformed UUID. See chat.SessionIDToChatID.
	if sessionID := canonicalSessionID(payload); sessionID != "" {
		attrs[attr.GenAIConversationIDKey] = sessionIDToUUID(sessionID).String()
	}
	if conv.PtrValOr(payload.Replayed, false) {
		// Downtime backlog redelivered from a device's offline spool: the
		// row's timestamp is the original occurred_at when the envelope
		// carried one (arrival time otherwise), so without this marker
		// replays would be indistinguishable from live traffic in
		// time-bucketed consumers.
		attrs[attr.HookReplayedKey] = true
	}
	if hostname := strings.TrimSpace(conv.PtrValOr(payload.Source.Hostname, "")); hostname != "" {
		attrs[attr.HookHostnameKey] = hostname
	}
	if model := canonicalModel(payload); model != "" {
		attrs[attr.GenAIResponseModelKey] = model
	}
	return attrs
}

func (s *Service) logHookTelemetry(ctx context.Context, authCtx *contextvalues.AuthContext, metadata *SessionMetadata, timestamp time.Time, toolName string, attrs map[attr.Key]any) {
	s.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp: timestamp,
		ToolInfo: telemetry.ToolInfo{
			Name:           toolName,
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      authCtx.ProjectID.String(),
			ID:             "",
			URN:            "",
			DeploymentID:   "",
			FunctionID:     nil,
		},
		UserInfo:   telemetry.UserInfoByIDAndEmail(metadata.UserID, metadata.UserEmail),
		Attributes: attrs,
	})
}

// telemetryHookEventName resolves the value stored in the gram.hook.event
// telemetry attribute. That attribute's vocabulary is the provider-style
// HookEvent names: the per-platform ingest endpoints have always written them,
// and the ClickHouse consumers (session summaries, tool-call success/failure
// counts, the is_completed_tool_call predicate) match on them. Canonical
// events are therefore translated back via the adapter's raw event name, with
// a fixed canonical fallback for senders that omit one, so unified-ingest rows
// keep counting without a ClickHouse migration.
func telemetryHookEventName(payload *gen.IngestPayload) string {
	// Skill activations are a Gram-specific classification layered onto an
	// ordinary provider tool event; resolving via the raw name would erase it.
	if isExplicitSkillActivation(payload) {
		return eventTypeSkillActivated
	}
	raw := strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, ""))
	if raw != "" {
		var parse func(string) (HookEvent, bool)
		switch strings.TrimSpace(payload.Source.Adapter) {
		case "claude":
			parse = parseClaudeHookEvent
		case "cursor":
			parse = parseCursorHookEvent
		case "codex":
			parse = parseCodexHookEvent
		case "opencode":
			parse = parseOpencodeHookEvent
		}
		if parse != nil {
			if event, ok := parse(raw); ok {
				return string(event)
			}
		}
	}
	switch eventType := strings.TrimSpace(payload.Event.Type); eventType {
	case "session.started":
		return string(HookEventSessionStart)
	case "session.ended":
		return string(HookEventSessionEnd)
	case "prompt.submitted":
		return string(HookEventUserPromptSubmit)
	case "tool.requested":
		return string(HookEventPreToolUse)
	case "tool.completed":
		return string(HookEventPostToolUse)
	case "tool.failed":
		return string(HookEventPostToolUseFailure)
	case "assistant.responded":
		return string(HookEventAfterAgentResponse)
	case "assistant.thought":
		return string(HookEventAfterAgentThought)
	case "notification.reported":
		return string(HookEventNotification)
	default:
		// session.updated, usage.reported, skill.activated and any future
		// canonical types have no provider-style equivalent; store them as-is.
		return eventType
	}
}

// persistCanonicalConversationEvent writes the event's chat row. occurredAt
// is the ingest-resolved canonicalEventTime, passed in rather than recomputed
// so the chat row, telemetry, and enforcement carry the exact same
// server-resolved time for one event — a recomputed fallback or clamp would
// drift by the handler's processing latency.
func (s *Service) persistCanonicalConversationEvent(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, metadata *SessionMetadata, hookSource string, occurredAt time.Time) (bool, error) {
	sessionID := canonicalSessionID(payload)
	if sessionID == "" || authCtx.ProjectID == nil {
		return false, nil
	}
	baseMsg := func(role, content string) chatRepo.CreateChatMessageParams {
		return chatRepo.CreateChatMessageParams{
			ChatID:           sessionIDToUUID(sessionID),
			ProjectID:        *authCtx.ProjectID,
			Role:             role,
			Content:          content,
			ContentRaw:       nil,
			ContentAssetUrl:  conv.ToPGTextEmpty(""),
			StorageError:     conv.ToPGTextEmpty(""),
			Model:            conv.ToPGTextEmpty(canonicalModel(payload)),
			MessageID:        conv.ToPGTextEmpty(""),
			ToolCallID:       conv.ToPGTextEmpty(""),
			UserID:           conv.ToPGTextEmpty(metadata.UserID),
			ExternalUserID:   conv.ToPGTextEmpty(metadata.UserEmail),
			FinishReason:     conv.ToPGTextEmpty(""),
			ToolCalls:        nil,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			Origin:           conv.ToPGTextEmpty(""),
			UserAgent:        conv.ToPGTextEmpty(""),
			IpAddress:        conv.ToPGTextEmpty(""),
			Source:           conv.ToPGTextEmpty(hookSource),
			ContentHash:      nil,
			Generation:       0,
			// Downtime backlog redelivered from a device's offline spool:
			// carried onto the row so the offline risk scanner's findings
			// (and any session view) can distinguish replayed traffic.
			Replayed:  conv.PtrValOr(payload.Replayed, false),
			CreatedAt: conv.ToPGTimestamptz(occurredAt),
		}
	}

	var msg chatRepo.CreateChatMessageParams
	var titleContent string
	uncorrelatedPrompt := false
	nativePrompt := false
	switch strings.TrimSpace(payload.Event.Type) {
	case "prompt.submitted":
		content := canonicalPromptText(payload)
		if strings.TrimSpace(content) == "" {
			return false, nil
		}
		msg = baseMsg("user", content)
		if correlationID := agentPromptCorrelationID(payload); correlationID != "" {
			msg.MessageID = conv.ToPGText(correlationID)
		} else {
			uncorrelatedPrompt = strings.EqualFold(strings.TrimSpace(hookSource), "litellm") || usesNativeTranscriptFallback(payload.Source.Adapter)
			nativePrompt = usesNativeTranscriptFallback(payload.Source.Adapter)
		}
		titleContent = content
	case "assistant.responded":
		content := canonicalMessageText(payload)
		outputToolCalls := authenticatedIngestOptions(ctx).OutputToolCalls
		if strings.TrimSpace(content) == "" && len(outputToolCalls) == 0 {
			return false, nil
		}
		msg = baseMsg("assistant", content)
		if len(outputToolCalls) > 0 {
			toolCallsJSON, err := json.Marshal(outputToolCalls)
			if err != nil {
				return false, fmt.Errorf("marshal output tool calls: %w", err)
			}
			msg.FinishReason = conv.ToPGText("tool_calls")
			msg.ToolCalls = toolCallsJSON
		}
		titleContent = content
	case "tool.requested":
		// Permission prompts (codex PermissionRequest) also normalize to
		// tool.requested but are only pre-approval previews: they may be
		// denied or followed by the real request, so persisting them would
		// put phantom or duplicate tool_calls rows in the transcript.
		if canonicalPermissionType(payload) != "" ||
			strings.EqualFold(strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, "")), "PermissionRequest") {
			return false, nil
		}
		toolName := canonicalToolName(payload)
		if strings.TrimSpace(toolName) == "" {
			return false, nil
		}
		toolCallsJSON, err := canonicalToolCallsJSON(payload)
		if err != nil {
			return false, err
		}
		msg = baseMsg("assistant", "")
		msg.FinishReason = conv.ToPGText("tool_calls")
		msg.ToolCalls = toolCallsJSON
		titleContent = toolName
	case "tool.completed", "tool.failed":
		content := canonicalToolResultContent(payload)
		if strings.TrimSpace(content) == "" {
			return false, nil
		}
		msg = baseMsg("tool", content)
		msg.ToolCallID = conv.ToPGTextEmpty(canonicalChatToolCallID(payload))
		titleContent = content
	default:
		return false, nil
	}

	title := canonicalChatTitle(payload, titleContent)
	if uncorrelatedPrompt {
		return s.insertUncorrelatedAgentPrompt(ctx, metadata, msg, title, nativePrompt)
	}
	stored, err := s.insertMessageWithFallbackUpsertResult(ctx, metadata, msg.ChatID, *authCtx.ProjectID, msg, title)
	return stored && msg.Role == "user", err
}

func usesNativeTranscriptFallback(adapter string) bool {
	switch strings.ToLower(strings.TrimSpace(adapter)) {
	case "claude", "claude-code", "claude-code-desktop", "cowork", "cursor":
		return true
	default:
		return false
	}
}

func (s *Service) markNativePromptSession(ctx context.Context, projectID, sessionID, source string) {
	if sessionID == "" {
		return
	}
	cacheCtx, cancel := context.WithTimeout(ctx, canonicalSessionCacheWriteTimeout)
	err := s.cache.Set(cacheCtx, sessionNativeHooksCacheKey(projectID, sessionID), source, 24*time.Hour)
	cancel()
	if err != nil {
		s.logger.WarnContext(ctx, "failed to mark native prompt session",
			attr.SlogError(err),
			attr.SlogGenAIConversationID(sessionID),
		)
	}
}

func agentPromptCorrelationID(payload *gen.IngestPayload) string {
	turnID := canonicalAgentTurnID(payload)
	if turnID == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(turnID))
	return agentPromptCorrelationPrefix + hex.EncodeToString(digest[:])
}

func canonicalAgentTurnID(payload *gen.IngestPayload) string {
	if payload == nil || payload.Source == nil {
		return ""
	}
	adapter := strings.ToLower(strings.TrimSpace(payload.Source.Adapter))
	if adapter != "codex" && adapter != "opencode" && adapter != "litellm" {
		return ""
	}
	if payload.Session != nil && payload.Session.TurnID != nil {
		turnID := strings.TrimSpace(*payload.Session.TurnID)
		if encoded, ok := strings.CutPrefix(turnID, agentTurnPrefix); ok {
			encodedProvider, nativeTurnID, found := strings.Cut(encoded, ":")
			encodedProvider = strings.ToLower(strings.TrimSpace(encodedProvider))
			stableProvider := encodedProvider == "codex" || encodedProvider == "opencode"
			if found && stableProvider && (adapter == "litellm" || adapter == encodedProvider) && strings.TrimSpace(nativeTurnID) != "" {
				return encodedProvider + ":" + strings.TrimSpace(nativeTurnID)
			}
			return ""
		}
		if adapter != "litellm" && turnID != "" {
			return adapter + ":" + turnID
		}
	}
	if adapter != "opencode" || payload.Raw == nil {
		return ""
	}

	raw, err := json.Marshal(payload.Raw)
	if err != nil {
		return ""
	}
	var event struct {
		Input struct {
			MessageID string `json:"messageID"`
		} `json:"input"`
		Output struct {
			Message struct {
				ID string `json:"id"`
			} `json:"message"`
		} `json:"output"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return ""
	}
	messageID := strings.TrimSpace(event.Output.Message.ID)
	if messageID == "" {
		messageID = strings.TrimSpace(event.Input.MessageID)
	}
	if messageID == "" {
		return ""
	}
	return "opencode:" + messageID
}

func (s *Service) persistPromptAttachments(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, metadata *SessionMetadata, occurredAt time.Time) error {
	if payload.Data == nil || len(payload.Data.PromptAttachments) == 0 || authCtx.ProjectID == nil {
		return nil
	}
	sessionID := canonicalSessionID(payload)
	if sessionID == "" {
		return nil
	}
	if s.productFeatures == nil {
		return nil
	}
	// A flag lookup failure defaults to off rather than failing the hook: the
	// capture is best effort, and the next turn re-ships anything skipped
	// because the high-water mark only advances on a successful read.
	enabled, err := s.productFeatures.IsFeatureEnabled(ctx, metadata.GramOrgID, productfeatures.FeatureSessionCapture)
	if err != nil {
		s.logger.WarnContext(ctx, "could not resolve session capture feature flag, skipping prompt attachments",
			attr.SlogError(err),
			attr.SlogOrganizationID(metadata.GramOrgID),
		)
		return nil
	}
	if !enabled {
		return nil
	}

	chatID := sessionIDToUUID(sessionID)
	projectID := *authCtx.ProjectID
	queries := chatRepo.New(s.db)

	parentResolver, err := newPromptAttachmentParentResolver(ctx, queries, chatID, projectID)
	if err != nil {
		return err
	}

	type pendingPromptAttachment struct {
		attachment *gen.HookPromptAttachmentEntry
		content    []byte
		parentID   uuid.NullUUID
		metadata   []byte
		createdAt  time.Time
	}
	pending := make([]pendingPromptAttachment, 0, len(payload.Data.PromptAttachments))
	for _, attachment := range payload.Data.PromptAttachments {
		if attachment == nil || strings.TrimSpace(attachment.EntryUUID) == "" || strings.TrimSpace(attachment.Content) == "" {
			continue
		}
		promptSHA256 := strings.ToLower(strings.TrimSpace(conv.PtrValOr(attachment.PromptSha256, "")))
		createdAt := occurredAt
		if ts := strings.TrimSpace(conv.PtrValOr(attachment.Timestamp, "")); ts != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				createdAt = parsed
			}
		}
		attachmentMetadata, err := promptAttachmentMetadata(attachment)
		if err != nil {
			return fmt.Errorf("marshal prompt attachment metadata: %w", err)
		}
		parentID := parentResolver.resolve(promptSHA256)
		pending = append(pending, pendingPromptAttachment{
			attachment: attachment,
			content:    []byte(attachment.Content),
			parentID:   parentID,
			metadata:   attachmentMetadata,
			createdAt:  createdAt,
		})
	}
	if len(pending) == 0 {
		return nil
	}

	contents := make([][]byte, len(pending))
	for i := range pending {
		contents[i] = pending[i].content
	}
	assetURLs, err := s.writer.WriteContentPartAssets(ctx, projectID, chatID, contents)
	if err != nil {
		return fmt.Errorf("store prompt attachment content assets: %w", err)
	}

	rows := make([]chatRepo.CreateChatContentPartParams, 0, len(pending))
	for i, pendingAttachment := range pending {
		rows = append(rows, chatRepo.CreateChatContentPartParams{
			ChatID:              chatID,
			ProjectID:           projectID,
			Kind:                message.PromptAttachment,
			ContentAssetUrl:     assetURLs[i],
			ExternalID:          conv.ToPGTextEmpty(strings.TrimSpace(pendingAttachment.attachment.EntryUUID)),
			ParentChatMessageID: pendingAttachment.parentID,
			Version:             pgtype.Int4{Int32: 0, Valid: false},
			Source:              conv.ToPGTextEmpty(strings.TrimSpace(payload.Source.Adapter)),
			Metadata:            pendingAttachment.metadata,
			RiskAnalyzedAt:      conv.PtrToPGTimestamptz(nil),
			CreatedAt:           conv.ToPGTimestamptz(pendingAttachment.createdAt),
		})
	}
	if _, err := queries.CreateChatContentPart(ctx, rows); err == nil {
		return nil
	} else if !isForeignKeyViolation(err) {
		return fmt.Errorf("insert prompt attachment content parts: %w", err)
	}
	_, upsertErr := s.repo.UpsertClaudeCodeSession(ctx, repo.UpsertClaudeCodeSessionParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: metadata.GramOrgID,
		UserID:         conv.ToPGTextEmpty(metadata.UserID),
		ExternalUserID: conv.ToPGTextEmpty(metadata.UserEmail),
		UserAccountID:  conv.StringToNullUUID(metadata.UserAccountID),
		Title:          conv.ToPGText(canonicalChatTitle(payload, "")),
	})
	if upsertErr != nil {
		return fmt.Errorf("upsert claude code session for prompt attachments: %w", upsertErr)
	}
	if _, err := queries.CreateChatContentPart(ctx, rows); err != nil {
		return fmt.Errorf("insert prompt attachment content parts after creating chat: %w", err)
	}
	return nil
}

type promptAttachmentParentResolver struct {
	latest uuid.NullUUID
	byHash map[string]uuid.NullUUID
}

func newPromptAttachmentParentResolver(ctx context.Context, queries *chatRepo.Queries, chatID uuid.UUID, projectID uuid.UUID) (*promptAttachmentParentResolver, error) {
	candidates, err := queries.ListClaudeUserMessagesForPromptAttachmentParent(ctx, chatRepo.ListClaudeUserMessagesForPromptAttachmentParentParams{
		ChatID:    chatID,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list prompt attachment parent candidates: %w", err)
	}
	resolver := &promptAttachmentParentResolver{
		latest: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		byHash: make(map[string]uuid.NullUUID, len(candidates)),
	}
	for i, candidate := range candidates {
		id := uuid.NullUUID{UUID: candidate.ID, Valid: true}
		if i == 0 {
			resolver.latest = id
		}
		sum := sha256.Sum256([]byte(strings.TrimSpace(candidate.Content)))
		hash := hex.EncodeToString(sum[:])
		if _, ok := resolver.byHash[hash]; !ok {
			resolver.byHash[hash] = id
		}
	}
	return resolver, nil
}

func (r *promptAttachmentParentResolver) resolve(promptSHA256 string) uuid.NullUUID {
	if r == nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	if promptSHA256 == "" {
		return r.latest
	}
	if id, ok := r.byHash[promptSHA256]; ok {
		return id
	}
	return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
}

// promptAttachmentMetadata packs the attachment's sparse descriptive fields
// into the attachment_metadata JSONB payload, or nil when there are none.
func promptAttachmentMetadata(attachment *gen.HookPromptAttachmentEntry) ([]byte, error) {
	metadata := map[string]string{}
	if displayPath := strings.TrimSpace(conv.PtrValOr(attachment.DisplayPath, "")); displayPath != "" {
		metadata["display_path"] = displayPath
	}
	if kind := strings.TrimSpace(attachment.AttachmentKind); kind != "" {
		metadata["kind"] = kind
	}
	if len(metadata) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal attachment metadata: %w", err)
	}
	return encoded, nil
}

func canonicalToolCallsJSON(payload *gen.IngestPayload) ([]byte, error) {
	toolCalls := []map[string]any{{
		"id":   canonicalChatToolCallID(payload),
		"type": "function",
		"function": map[string]any{
			"name":      canonicalToolName(payload),
			"arguments": marshalToJSON(canonicalToolInput(payload)),
		},
	}}
	toolCallsJSON, err := json.Marshal(toolCalls)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical tool_calls: %w", err)
	}
	return toolCallsJSON, nil
}

// canonicalChatToolCallID falls back to the shared per-(session, tool) key
// when the sender supplied no per-call id, matching canonicalTraceID's
// fallback: hashing the recorded id must reproduce the trace id, or the
// shadow-MCP provenance lookup can never join the recorded call to its hook
// log (DNO-604).
func canonicalChatToolCallID(payload *gen.IngestPayload) string {
	if id := canonicalToolCallID(payload); id != "" {
		return id
	}
	if key := canonicalSyntheticToolCallID(payload); key != "" {
		return key
	}
	if name := canonicalToolName(payload); name != "" {
		return name
	}
	return canonicalTraceID(payload)
}

// canonicalSyntheticToolCallID returns the shared per-(session, tool) fallback
// key for tool events only. canonicalTraceID runs for every ingested event
// (hookTelemetryBaseAttrs), and the schema permits tool_call data on any
// event, but only tool events have a recorded chat side to join — a
// skill.activated or prompt row carrying a tool name must keep its
// session-level trace instead of migrating into the tool's trace.
func canonicalSyntheticToolCallID(payload *gen.IngestPayload) string {
	if payload == nil || payload.Event == nil {
		return ""
	}
	switch strings.TrimSpace(payload.Event.Type) {
	case "tool.requested", "tool.completed", "tool.failed":
		return syntheticToolCallID(canonicalSessionID(payload), canonicalToolName(payload))
	default:
		return ""
	}
}

func canonicalToolResultContent(payload *gen.IngestPayload) string {
	if strings.TrimSpace(payload.Event.Type) == "tool.failed" {
		return marshalToJSON(canonicalToolError(payload))
	}
	if mcp := canonicalMCPData(payload); mcp != nil && mcp.ResultJSON != nil {
		return strings.TrimSpace(*mcp.ResultJSON)
	}
	return marshalToJSON(canonicalToolOutput(payload))
}

func canonicalAllowResult() *gen.IngestHookResult {
	return &gen.IngestHookResult{
		Decision: "allow",
		Reason:   nil,
		Message:  nil,
		Effects:  nil,
	}
}

func canonicalDenyResult(message string) *gen.IngestHookResult {
	if strings.TrimSpace(message) == "" {
		message = "Request denied by Speakeasy policy."
	}
	reason := "policy_denied"
	return &gen.IngestHookResult{
		Decision: "deny",
		Reason:   &reason,
		Message:  &message,
		Effects:  nil,
	}
}

// maxEventBackdate bounds how far into the past a sender-supplied
// occurred_at may reach. It mirrors the client spool's 14-day expiry: no
// legitimate replay is older, so anything beyond it is a skewed or hostile
// clock that would otherwise sort a row ahead of the entire transcript
// forever (occurred_at is fully client-controlled).
const maxEventBackdate = 14 * 24 * time.Hour

// canonicalEventTime returns the event's occurred_at clamped to
// [now-maxEventBackdate, now]. The clamp lives here — not at individual
// persistence sites — so every consumer (chat rows, ClickHouse telemetry,
// enforcement evaluation) agrees on one time for one event; a skewed device
// clock must never make the stores diverge.
func canonicalEventTime(payload *gen.IngestPayload) time.Time {
	now := time.Now()
	if payload != nil && payload.Event != nil {
		if raw := strings.TrimSpace(conv.PtrValOr(payload.Event.OccurredAt, "")); raw != "" {
			if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				if t.After(now) {
					return now
				}
				if floor := now.Add(-maxEventBackdate); t.Before(floor) {
					return floor
				}
				return t
			}
		}
	}
	return now
}

func canonicalSessionID(payload *gen.IngestPayload) string {
	if payload != nil && payload.Session != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Session.ID, ""))
	}
	return ""
}

func canonicalModel(payload *gen.IngestPayload) string {
	if payload != nil && payload.Session != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Session.Model, ""))
	}
	return ""
}

func canonicalToolName(payload *gen.IngestPayload) string {
	if tool := canonicalToolCallData(payload); tool != nil {
		return strings.TrimSpace(conv.PtrValOr(tool.Name, ""))
	}
	return ""
}

func canonicalTelemetryToolName(payload *gen.IngestPayload) string {
	// Only explicit skill.activated events are relabeled: an inferred skill on
	// an ordinary tool/prompt event keeps the event's own tool identity and
	// gets a separate derived skill row instead.
	if skill := canonicalSkillName(payload); skill != "" && isExplicitSkillActivation(payload) {
		return "Skill"
	}
	return canonicalToolName(payload)
}

func canonicalToolCallID(payload *gen.IngestPayload) string {
	if tool := canonicalToolCallData(payload); tool != nil {
		return strings.TrimSpace(conv.PtrValOr(tool.ID, ""))
	}
	return ""
}

func canonicalTraceID(payload *gen.IngestPayload) string {
	if id := canonicalToolCallID(payload); id != "" {
		return hashToolCallIDToTraceID(id)
	}
	// Tool events without a per-call id trace per (session, tool), keeping the
	// trace id derivable from the id canonicalChatToolCallID records (DNO-604).
	if key := canonicalSyntheticToolCallID(payload); key != "" {
		return hashToolCallIDToTraceID(key)
	}
	if sessionID := canonicalSessionID(payload); sessionID != "" {
		return hashToolCallIDToTraceID(sessionID)
	}
	return generateTraceID()
}

func canonicalToolInput(payload *gen.IngestPayload) any {
	if tool := canonicalToolCallData(payload); tool != nil {
		return tool.Input
	}
	return nil
}

func canonicalToolOutput(payload *gen.IngestPayload) any {
	if tool := canonicalToolCallData(payload); tool != nil && tool.Output != nil {
		return tool.Output
	}
	if mcp := canonicalMCPData(payload); mcp != nil && mcp.ResultJSON != nil {
		return *mcp.ResultJSON
	}
	return nil
}

func canonicalToolError(payload *gen.IngestPayload) any {
	if tool := canonicalToolCallData(payload); tool != nil {
		return tool.Error
	}
	return nil
}

func canonicalIsInterrupt(payload *gen.IngestPayload) *bool {
	if tool := canonicalToolCallData(payload); tool != nil {
		return tool.IsInterrupt
	}
	return nil
}

func canonicalPermissionType(payload *gen.IngestPayload) string {
	if tool := canonicalToolCallData(payload); tool != nil {
		return strings.TrimSpace(conv.PtrValOr(tool.PermissionType, ""))
	}
	return ""
}

func canonicalPromptText(payload *gen.IngestPayload) string {
	if payload != nil && payload.Data != nil && payload.Data.Prompt != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Data.Prompt.Text, ""))
	}
	return ""
}

func canonicalMessageText(payload *gen.IngestPayload) string {
	if payload != nil && payload.Data != nil && payload.Data.Message != nil {
		return strings.TrimSpace(conv.PtrValOr(payload.Data.Message.Text, ""))
	}
	return ""
}

// canonicalSkillName strips a single `<scope>:` plugin prefix from the
// reported skill name so activations attribute to one canonical skill no
// matter which plugin distributed it.
func canonicalSkillName(payload *gen.IngestPayload) string {
	if payload == nil || payload.Data == nil || payload.Data.Skill == nil {
		return ""
	}
	name := strings.TrimSpace(payload.Data.Skill.Name)
	if scope, rest, scoped := strings.Cut(name, ":"); scoped && strings.TrimSpace(scope) != "" && !strings.Contains(rest, ":") {
		name = strings.TrimSpace(rest)
	}
	return name
}

func canonicalChatTitle(payload *gen.IngestPayload, fallback string) string {
	title := canonicalPromptText(payload)
	if title == "" {
		title = fallback
	}
	title = strings.TrimSpace(title)
	runes := []rune(title)
	if len(runes) <= 80 {
		return title
	}
	return string(runes[:80])
}

func canonicalToolCallData(payload *gen.IngestPayload) *gen.HookToolCallData {
	if payload != nil && payload.Data != nil {
		return payload.Data.ToolCall
	}
	return nil
}

func canonicalMCPData(payload *gen.IngestPayload) *gen.HookMCPData {
	if payload != nil && payload.Data != nil {
		return payload.Data.Mcp
	}
	return nil
}

func canonicalMCPInventoryEntries(payload *gen.IngestPayload) []MCPServerEntry {
	if payload == nil || payload.Data == nil || payload.Data.McpInventory == nil {
		return nil
	}
	adapter := strings.TrimSpace(payload.Source.Adapter)
	isCodex := strings.EqualFold(adapter, "codex")
	entries := make([]MCPServerEntry, 0, len(payload.Data.McpInventory))
	for _, mcp := range payload.Data.McpInventory {
		if mcp == nil {
			continue
		}
		name := strings.TrimSpace(conv.PtrValOr(mcp.ServerName, ""))
		// Codex addresses a server by its sanitized tool prefix as well as its
		// configured name, and the prefix is the only thing the cached-entry
		// fallback matches on. Leaving it empty makes a hyphenated server
		// ("platform-logs", addressed as "platform_logs") unresolvable here
		// while the legacy endpoint resolves it — a Gram-hosted target would
		// be denied. Mirrors ParseCodexMCPList.
		toolPrefix := ""
		if isCodex {
			toolPrefix = codexSanitizeToolName(name)
		}
		entries = append(entries, MCPServerEntry{
			RawLine:       "",
			Source:        adapter,
			PluginName:    "",
			Name:          name,
			URL:           strings.TrimSpace(conv.PtrValOr(mcp.URL, "")),
			Command:       strings.TrimSpace(conv.PtrValOr(mcp.Command, "")),
			Transport:     "",
			Status:        "unknown",
			StatusRaw:     "",
			ConnectorUUID: "",
			ToolPrefix:    toolPrefix,
		})
	}
	return entries
}

func canonicalUsageData(payload *gen.IngestPayload) *gen.HookUsageData {
	if payload != nil && payload.Data != nil {
		return payload.Data.Usage
	}
	return nil
}

func jsonString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case json.RawMessage:
		return string(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}
