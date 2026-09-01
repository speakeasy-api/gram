package litellm

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/hooks/delegation"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/litellmacting"
)

const (
	actingPrincipalHeader         = "x-gram-acting-principal"
	actingPrincipalContractHeader = "x-gram-acting-principal-contract"
	inferenceInvocationHeader     = "x-gram-inference-invocation-id"
	liteLLMAIAccessTimeout        = 5 * time.Second
)

type liteLLMAIAccessCheckpointer interface {
	Evaluate(context.Context, *gen.IngestPayload, *contextvalues.AuthContext) liteLLMAIAccessDecision
}

type liteLLMAIAccessEvaluator interface {
	Evaluate(context.Context, killswitches.EvaluationRequest) killswitches.EvaluationResult
}

type LiteLLMAIAccessCheckpoint struct {
	principal killswitches.PrincipalAdapter
	resource  killswitches.ResourceAdapter
	evaluator liteLLMAIAccessEvaluator
	transport killswitches.TransportAdapter
	verifier  *litellmacting.Signer
}

type liteLLMAIAccessDecision struct {
	blocked bool
	reason  string
	message string
	userID  string
}

func NewLiteLLMAIAccessCheckpoint(registry *killswitches.Registry, evaluator liteLLMAIAccessEvaluator, verifier *litellmacting.Signer) (*LiteLLMAIAccessCheckpoint, error) {
	if registry == nil || evaluator == nil || verifier == nil {
		return nil, errors.New("LiteLLM ai_access registry, evaluator, and verifier are required")
	}
	principal, ok := registry.PrincipalAdapter(mcptoolexecution.PrincipalKindUser)
	if !ok {
		return nil, errors.New("authenticated user principal adapter is not registered")
	}
	resource, ok := registry.ResourceAdapter(mcptoolexecution.ResourceKindLiteLLMInstance)
	if !ok {
		return nil, errors.New("LiteLLM instance resource adapter is not registered")
	}
	transport, ok := registry.TransportAdapter(mcptoolexecution.TransportAdapterLiteLLMGenericGuardrail)
	if !ok {
		return nil, errors.New("LiteLLM Generic Guardrail transport adapter is not registered")
	}
	coverage, ok := registry.Coverage(mcptoolexecution.DefinitionKeyAIAccess, mcptoolexecution.SurfaceLiteLLMPreInference)
	if !ok || coverage.FailurePolicy != killswitches.FailurePolicyFailClosed || coverage.TransportAdapter != mcptoolexecution.TransportAdapterLiteLLMGenericGuardrail {
		return nil, errors.New("fail-closed LiteLLM pre-inference ai_access coverage is not registered")
	}
	return &LiteLLMAIAccessCheckpoint{principal: principal, resource: resource, evaluator: evaluator, transport: transport, verifier: verifier}, nil
}

func liteLLMIdentityFailureDecision() liteLLMAIAccessDecision {
	return liteLLMAIAccessDecision{blocked: true, reason: "ai_access_identity_unavailable", message: delegation.IdentityFailureMessage, userID: ""}
}

func liteLLMEvaluatorFailureDecision() liteLLMAIAccessDecision {
	return liteLLMAIAccessDecision{blocked: true, reason: "ai_access_evaluator_unavailable", message: delegation.EvaluatorFailureMessage, userID: ""}
}

// Evaluate performs the dedicated ai_access checkpoint for every pre-call
// callback. It runs before any content extraction and performs no allow cache.
func (c *LiteLLMAIAccessCheckpoint) Evaluate(ctx context.Context, payload *gen.IngestPayload, authCtx *contextvalues.AuthContext) liteLLMAIAccessDecision {
	if c == nil || payload == nil || authCtx == nil || authCtx.ProjectID == nil || strings.TrimSpace(authCtx.ActiveOrganizationID) == "" {
		return liteLLMIdentityFailureDecision()
	}
	apiKeyID, err := uuid.Parse(authCtx.APIKeyID)
	if err != nil || apiKeyID == uuid.Nil || apiKeyID.String() != authCtx.APIKeyID {
		return liteLLMIdentityFailureDecision()
	}
	headers, ok := actingPrincipalHeaders(payload.RequestHeaders)
	if !ok || headers.contract != litellmacting.ContractVersion {
		return liteLLMIdentityFailureDecision()
	}

	organizationID := killswitches.OrganizationID(authCtx.ActiveOrganizationID)
	evalCtx, cancel := context.WithTimeout(ctx, liteLLMAIAccessTimeout)
	defer cancel()

	resourceResult, err := c.resource.Derive(evalCtx, organizationID, mcptoolexecution.LiteLLMInstanceSource{ProjectID: *authCtx.ProjectID, APIKeyID: apiKeyID})
	if err != nil {
		return liteLLMIdentityFailureDecision()
	}
	resourceKey, supported, err := resourceResult.Key()
	if err != nil || !supported {
		return liteLLMIdentityFailureDecision()
	}

	identity, err := c.verifier.VerifyAssertion(headers.assertion, litellmacting.AssertionBinding{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      authCtx.ProjectID.String(),
		InstanceID:     string(resourceKey),
		APIKeyID:       apiKeyID.String(),
		InvocationID:   headers.invocationID,
	})
	if err != nil {
		return liteLLMIdentityFailureDecision()
	}
	principalResult, err := c.principal.Canonicalize(organizationID, identity.UserID)
	if err != nil {
		return liteLLMEvaluatorFailureDecision()
	}
	principalKey, supported, err := principalResult.Key()
	if err != nil || !supported {
		return liteLLMIdentityFailureDecision()
	}
	current, err := c.principal.ValidateCurrentOrganization(evalCtx, organizationID, principalKey)
	if err != nil {
		return liteLLMEvaluatorFailureDecision()
	}
	if !current {
		return liteLLMIdentityFailureDecision()
	}

	result := c.evaluator.Evaluate(evalCtx, killswitches.EvaluationRequest{
		OrganizationID:      organizationID,
		DefinitionKeys:      []killswitches.DefinitionKey{mcptoolexecution.DefinitionKeyAIAccess},
		PrincipalCandidates: []killswitches.PrincipalCandidate{{Kind: mcptoolexecution.PrincipalKindUser, Key: principalKey}},
		ResourceKind:        mcptoolexecution.ResourceKindLiteLLMInstance,
		ResourceKey:         resourceKey,
	})
	disposition, err := c.transport(result, killswitches.FailurePolicyFailClosed)
	if err != nil {
		return liteLLMEvaluatorFailureDecision()
	}
	switch disposition.Kind() {
	case killswitches.TransportDispositionContinue:
		return liteLLMAIAccessDecision{blocked: false, reason: "", message: "", userID: identity.UserID}
	case killswitches.TransportDispositionMatchedDenial:
		note, ok := disposition.ExternalNote()
		if !ok || strings.TrimSpace(note) == "" {
			return liteLLMEvaluatorFailureDecision()
		}
		return liteLLMAIAccessDecision{blocked: true, reason: "ai_access_denied", message: note, userID: identity.UserID}
	default:
		return liteLLMEvaluatorFailureDecision()
	}
}

type actingHeaders struct {
	assertion    string
	contract     string
	invocationID string
}

func actingPrincipalHeaders(input map[string]string) (actingHeaders, bool) {
	values := map[string]string{}
	for key, value := range input {
		canonical := strings.ToLower(strings.TrimSpace(key))
		if canonical != actingPrincipalHeader && canonical != actingPrincipalContractHeader && canonical != inferenceInvocationHeader {
			continue
		}
		if _, duplicate := values[canonical]; duplicate {
			return actingHeaders{assertion: "", contract: "", invocationID: ""}, false
		}
		values[canonical] = strings.TrimSpace(value)
	}
	result := actingHeaders{assertion: values[actingPrincipalHeader], contract: values[actingPrincipalContractHeader], invocationID: values[inferenceInvocationHeader]}
	return result, result.assertion != "" && result.contract != "" && result.invocationID != ""
}

func (d liteLLMAIAccessDecision) result() *gen.LitellmIngestResult {
	if !d.blocked {
		return nil
	}
	return &gen.LitellmIngestResult{
		Action: "BLOCKED", BlockedReason: &d.message, Texts: nil, Images: nil, Tools: nil, StreamHoldbackChars: nil,
	}
}
