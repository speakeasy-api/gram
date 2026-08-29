package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/hooks/delegation"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hooksacting"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
)

const (
	aiAccessIdentityFailureMessage  = delegation.IdentityFailureMessage
	aiAccessEvaluatorFailureMessage = "Speakeasy could not confirm the current AI access policy. Try again."
	aiAccessDenialCacheTTL          = 10 * time.Minute
)

type hookAIAccessEvaluator interface {
	Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult
}

// HookAIAccessCheckpoint is the unified hook's injected DNO-988 registry and
// evaluator checkpoint. It owns no evaluator, database pool, or allow cache.
type HookAIAccessCheckpoint struct {
	principal killswitches.PrincipalAdapter
	resource  killswitches.ResourceAdapter
	evaluator hookAIAccessEvaluator
	transport killswitches.TransportAdapter
	verifier  *hooksacting.Signer
	policy    killswitches.FailurePolicy
}

func NewHookAIAccessCheckpoint(registry *killswitches.Registry, evaluator hookAIAccessEvaluator, verifier *hooksacting.Signer) (*HookAIAccessCheckpoint, error) {
	if registry == nil || evaluator == nil || verifier == nil {
		return nil, errors.New("hook ai_access registry, evaluator, and assertion verifier are required")
	}
	principal, ok := registry.PrincipalAdapter(mcptoolexecution.PrincipalKindUser)
	if !ok {
		return nil, errors.New("authenticated user principal adapter is not registered")
	}
	resource, ok := registry.ResourceAdapter(mcptoolexecution.ResourceKindHookActivity)
	if !ok {
		return nil, errors.New("hook activity resource adapter is not registered")
	}
	transport, ok := registry.TransportAdapter(mcptoolexecution.TransportAdapterHookNative)
	if !ok {
		return nil, errors.New("hook native transport adapter is not registered")
	}
	coverage, ok := registry.Coverage(mcptoolexecution.DefinitionKeyAIAccess, mcptoolexecution.SurfaceClaudeUserPromptSubmit)
	if !ok {
		return nil, errors.New("hook ai_access coverage is not registered")
	}
	return &HookAIAccessCheckpoint{principal: principal, resource: resource, evaluator: evaluator, transport: transport, verifier: verifier, policy: coverage.FailurePolicy}, nil
}

type hookAIAccessDecision struct {
	governed bool
	deny     bool
	reason   string
	message  string
}

func governedHook(payload *gen.IngestPayload) (provider, event string, governed bool) {
	if payload == nil || payload.Source == nil {
		return "", "", false
	}
	provider = strings.ToLower(strings.TrimSpace(payload.Source.Adapter))
	event = strings.TrimSpace(conv.PtrValOr(payload.Source.RawEventName, ""))
	return provider, event, delegation.Approved(provider, event)
}

func validLiveGovernedHook(payload *gen.IngestPayload, event string) bool {
	if payload == nil || payload.Event == nil || conv.PtrValOr(payload.Replayed, false) || conv.PtrValOr(payload.Backfilled, false) {
		return false
	}
	canonicalType := strings.TrimSpace(payload.Event.Type)
	return (event == delegation.EventUserPromptSubmit && canonicalType == "prompt.submitted") ||
		(event == delegation.EventPreToolUse && canonicalType == "tool.requested")
}

type verifiedHookAIAccess struct {
	organizationID killswitches.OrganizationID
	principalKey   killswitches.PrincipalKey
	resourceKey    killswitches.ResourceKey
}

func identityFailureDecision() hookAIAccessDecision {
	return hookAIAccessDecision{governed: true, deny: true, reason: "ai_access_identity_unavailable", message: aiAccessIdentityFailureMessage}
}

func evaluatorFailureDecision() hookAIAccessDecision {
	return hookAIAccessDecision{governed: true, deny: true, reason: "ai_access_evaluator_unavailable", message: aiAccessEvaluatorFailureMessage}
}

// Verify authenticates and canonicalizes the acting identity and resource. It
// always revalidates active membership before any denial-cache lookup.
//
//nolint:exhaustruct // Empty outcomes intentionally encode unverified and ungoverned state-machine branches.
func (c *HookAIAccessCheckpoint) Verify(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) (verifiedHookAIAccess, hookAIAccessDecision) {
	provider, event, governed := governedHook(payload)
	if !governed {
		return verifiedHookAIAccess{}, hookAIAccessDecision{}
	}
	if c == nil || authCtx == nil || strings.TrimSpace(authCtx.ActiveOrganizationID) == "" || payload == nil {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	if !validLiveGovernedHook(payload, event) {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	assertion := strings.TrimSpace(conv.PtrValOr(payload.ActingUserAssertion, ""))
	contract := strings.TrimSpace(conv.PtrValOr(payload.ActingUserContractVersion, ""))
	sessionID := canonicalSessionID(payload)
	idempotencyKey := strings.TrimSpace(conv.PtrValOr(payload.IdempotencyKey, ""))
	if assertion == "" || contract != delegation.ContractVersion || sessionID == "" || idempotencyKey == "" {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	verified, err := c.verifier.VerifyAssertion(assertion, hooksacting.AssertionBinding{
		OrganizationID: authCtx.ActiveOrganizationID, Provider: provider, Event: event, SessionID: sessionID, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	organizationID := killswitches.OrganizationID(authCtx.ActiveOrganizationID)
	canonicalPrincipal, err := c.principal.Canonicalize(organizationID, verified.UserID)
	if err != nil {
		return verifiedHookAIAccess{}, evaluatorFailureDecision()
	}
	principalKey, supported, err := canonicalPrincipal.Key()
	if err != nil || !supported {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	return c.revalidate(ctx, organizationID, principalKey, provider, event)
}

// VerifyCached revalidates current membership and resource ownership for an
// exact previously denied request. The cache key binds the raw assertion, so
// an expired assertion can be retried without letting a modified assertion
// inherit the stored principal or decision.
//
//nolint:exhaustruct // Empty outcomes intentionally encode unverified and ungoverned state-machine branches.
func (c *HookAIAccessCheckpoint) VerifyCached(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext, principalKey killswitches.PrincipalKey) (verifiedHookAIAccess, hookAIAccessDecision) {
	provider, event, governed := governedHook(payload)
	if !governed {
		return verifiedHookAIAccess{}, hookAIAccessDecision{}
	}
	if c == nil || authCtx == nil || strings.TrimSpace(authCtx.ActiveOrganizationID) == "" || principalKey == "" || !validLiveGovernedHook(payload, event) {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	organizationID := killswitches.OrganizationID(authCtx.ActiveOrganizationID)
	return c.revalidate(ctx, organizationID, principalKey, provider, event)
}

//nolint:exhaustruct // Empty outcomes intentionally encode identity and evaluator failure branches.
func (c *HookAIAccessCheckpoint) revalidate(ctx context.Context, organizationID killswitches.OrganizationID, principalKey killswitches.PrincipalKey, provider, event string) (verifiedHookAIAccess, hookAIAccessDecision) {
	active, err := c.principal.ValidateCurrentOrganization(ctx, organizationID, principalKey)
	if err != nil {
		return verifiedHookAIAccess{}, evaluatorFailureDecision()
	}
	if !active {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	resourceInput, ok := delegation.ResourceKey(provider, event)
	if !ok {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	canonicalResource, err := c.resource.Canonicalize(organizationID, resourceInput)
	if err != nil {
		return verifiedHookAIAccess{}, evaluatorFailureDecision()
	}
	resourceKey, supported, err := canonicalResource.Key()
	if err != nil || !supported {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	current, err := c.resource.ValidateCurrentOrganization(ctx, organizationID, resourceKey)
	if err != nil {
		return verifiedHookAIAccess{}, evaluatorFailureDecision()
	}
	if !current {
		return verifiedHookAIAccess{}, identityFailureDecision()
	}
	return verifiedHookAIAccess{organizationID: organizationID, principalKey: principalKey, resourceKey: resourceKey}, hookAIAccessDecision{}
}

func (c *HookAIAccessCheckpoint) EvaluateVerified(ctx context.Context, verified verifiedHookAIAccess) hookAIAccessDecision {
	result := c.evaluator.Evaluate(ctx, killswitches.EvaluationRequest{
		OrganizationID: verified.organizationID, DefinitionKeys: []killswitches.DefinitionKey{mcptoolexecution.DefinitionKeyAIAccess},
		PrincipalCandidates: []killswitches.PrincipalCandidate{{Kind: mcptoolexecution.PrincipalKindUser, Key: verified.principalKey}},
		ResourceKind:        mcptoolexecution.ResourceKindHookActivity, ResourceKey: verified.resourceKey,
	})
	disposition, err := c.transport(result, c.policy)
	if err != nil {
		return evaluatorFailureDecision()
	}
	switch disposition.Kind() {
	case killswitches.TransportDispositionContinue:
		return hookAIAccessDecision{governed: true, deny: false, reason: "", message: ""}
	case killswitches.TransportDispositionMatchedDenial:
		note, ok := disposition.ExternalNote()
		if !ok || strings.TrimSpace(note) == "" {
			return evaluatorFailureDecision()
		}
		return hookAIAccessDecision{governed: true, deny: true, reason: "ai_access_denied", message: note}
	default:
		return evaluatorFailureDecision()
	}
}

type cachedHookDenial struct {
	PrincipalKey killswitches.PrincipalKey `json:"principal_key"`
	Reason       string                    `json:"reason"`
	Message      string                    `json:"message"`
}

func hookDenialCacheKey(payload *gen.IngestPayload, organizationID string) (string, bool) {
	provider, event, governed := governedHook(payload)
	idempotencyKey := strings.TrimSpace(conv.PtrValOr(payload.IdempotencyKey, ""))
	sessionID := canonicalSessionID(payload)
	contractVersion := strings.TrimSpace(conv.PtrValOr(payload.ActingUserContractVersion, ""))
	assertion := strings.TrimSpace(conv.PtrValOr(payload.ActingUserAssertion, ""))
	if !governed || organizationID == "" || idempotencyKey == "" || sessionID == "" || contractVersion == "" || assertion == "" {
		return "", false
	}
	// Hash a length-unambiguous binding so untrusted separators cannot alias.
	digest := sha256.New()
	assertionDigest := sha256.Sum256([]byte(assertion))
	for _, value := range []string{organizationID, provider, event, sessionID, idempotencyKey, delegation.AssertionAudience, contractVersion, hex.EncodeToString(assertionDigest[:])} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write([]byte(value))
	}
	return "hook:ai-access:denial:v1:" + hex.EncodeToString(digest.Sum(nil)), true
}

//nolint:exhaustruct // Empty decisions intentionally represent cache misses in this state machine.
func (s *Service) cachedHookAIAccessDenial(ctx context.Context, payload *gen.IngestPayload, organizationID string) (hookAIAccessDecision, killswitches.PrincipalKey, bool) {
	key, ok := hookDenialCacheKey(payload, organizationID)
	if !ok {
		return hookAIAccessDecision{}, "", false
	}
	var cached cachedHookDenial
	if err := s.cache.Get(ctx, key, &cached); err != nil || cached.PrincipalKey == "" || cached.Reason != "ai_access_denied" || strings.TrimSpace(cached.Message) == "" {
		return hookAIAccessDecision{}, "", false
	}
	return hookAIAccessDecision{governed: true, deny: true, reason: cached.Reason, message: cached.Message}, cached.PrincipalKey, true
}

func (s *Service) publishHookAIAccessDenial(ctx context.Context, payload *gen.IngestPayload, organizationID string, principalKey killswitches.PrincipalKey, decision hookAIAccessDecision) hookAIAccessDecision {
	if !decision.deny || decision.reason != "ai_access_denied" || strings.TrimSpace(decision.message) == "" {
		return decision
	}
	key, ok := hookDenialCacheKey(payload, organizationID)
	conditional, supported := s.cache.(cache.ConditionalCache)
	if !ok || !supported {
		return evaluatorFailureDecision()
	}
	record := cachedHookDenial{PrincipalKey: principalKey, Reason: decision.reason, Message: decision.message}
	stored, err := conditional.SetIfAbsent(ctx, key, record, aiAccessDenialCacheTTL)
	if err != nil {
		return evaluatorFailureDecision()
	}
	if stored {
		return decision
	}
	var first cachedHookDenial
	if err := s.cache.Get(ctx, key, &first); err != nil || first.PrincipalKey == "" || first.Reason != "ai_access_denied" || strings.TrimSpace(first.Message) == "" {
		return evaluatorFailureDecision()
	}
	return hookAIAccessDecision{governed: true, deny: true, reason: first.Reason, message: first.Message}
}
