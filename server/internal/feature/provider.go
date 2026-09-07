package feature

import (
	"context"
	"fmt"
	"sync"
)

type Provider interface {
	// IsFlagEnabled reports whether flag is enabled for the given distinctID.
	// groups carries PostHog group memberships (group type -> group key) so
	// that group-targeted flag releases evaluate correctly server-side; pass
	// nil when the flag is targeted purely by distinct ID. Use
	// OrgProjectGroups to build the org/project groups the dashboard registers.
	IsFlagEnabled(ctx context.Context, flag Flag, distinctID string, groups map[string]string) (bool, error)

	// IsFlagEnabledLocal evaluates a flag using only locally cached flag
	// definitions. Providers must fail closed without falling back to remote
	// evaluation when the local result is unavailable or inconclusive.
	IsFlagEnabledLocal(ctx context.Context, flag Flag, distinctID string, groups, personProperties map[string]string) (bool, error)

	// FlagPayload returns the raw JSON payload PostHog attaches to the flag
	// release that matches distinctID, or (nil, nil) when the flag is off, has
	// no payload, or the provider is disabled. groups is used for group-targeted
	// releases the same way as IsFlagEnabled. Callers should fail closed: treat a
	// nil payload or an error as "no clearance".
	FlagPayload(ctx context.Context, flag Flag, distinctID string, groups map[string]string) ([]byte, error)
}

type InMemory sync.Map

func (imp *InMemory) IsFlagEnabled(ctx context.Context, flag Flag, distinctID string, groups map[string]string) (bool, error) {
	key := distinctID + ":" + string(flag)

	val, ok := (*sync.Map)(imp).Load(key)
	if !ok {
		return false, nil
	}

	enabled, ok := val.(bool)
	if !ok {
		return false, nil
	}

	return enabled, nil
}

func (imp *InMemory) IsFlagEnabledLocal(ctx context.Context, flag Flag, distinctID string, groups, personProperties map[string]string) (bool, error) {
	return imp.IsFlagEnabled(ctx, flag, distinctID, groups)
}

func (imp *InMemory) SetFlag(flag Flag, distinctID string, enabled bool) {
	key := distinctID + ":" + string(flag)

	(*sync.Map)(imp).Store(key, enabled)
}

// payloadKey namespaces payload entries so they never collide with the boolean
// entries SetFlag/IsFlagEnabled store under "<distinctID>:<flag>".
func payloadKey(flag Flag, distinctID string) string {
	return "payload:" + distinctID + ":" + string(flag)
}

func (imp *InMemory) FlagPayload(ctx context.Context, flag Flag, distinctID string, groups map[string]string) ([]byte, error) {
	val, ok := (*sync.Map)(imp).Load(payloadKey(flag, distinctID))
	if !ok {
		return nil, nil
	}

	payload, ok := val.([]byte)
	if !ok {
		return nil, nil
	}

	return payload, nil
}

func (imp *InMemory) SetFlagPayload(flag Flag, distinctID string, payload []byte) {
	(*sync.Map)(imp).Store(payloadKey(flag, distinctID), payload)
}

// variantKey namespaces variant entries so they never collide with the boolean
// or payload entries stored for the same flag.
func variantKey(flag Flag, distinctID string) string {
	return "variant:" + distinctID + ":" + string(flag)
}

func (imp *InMemory) FlagVariant(ctx context.Context, flag Flag, distinctID string, groups map[string]string) (Variant, error) {
	val, ok := (*sync.Map)(imp).Load(variantKey(flag, distinctID))
	if !ok {
		return "", nil
	}

	variant, ok := val.(Variant)
	if !ok {
		return "", nil
	}

	return variant, nil
}

func (imp *InMemory) SetFlagVariant(flag Flag, distinctID string, variant Variant) {
	(*sync.Map)(imp).Store(variantKey(flag, distinctID), variant)
}

// Variant is the release key a multivariate flag resolves to. An empty Variant
// means the flag is off, boolean-only, missing, or the provider could not
// decide — callers must map it to their own fail-safe default rather than
// treating it as a distinct behaviour.
type Variant string

// VariantProvider is implemented by providers that can resolve a multivariate
// flag's variant key. Kept separate from Provider so bool-only implementations
// (test fakes included) keep compiling.
type VariantProvider interface {
	FlagVariant(ctx context.Context, flag Flag, distinctID string, groups map[string]string) (Variant, error)
}

// FlagVariant resolves a multivariate flag's variant, returning "" for
// providers that only expose the bool contract or when no variant matched.
func FlagVariant(ctx context.Context, provider Provider, flag Flag, distinctID string, groups map[string]string) (Variant, error) {
	if provider == nil {
		return "", nil
	}
	resolver, ok := provider.(VariantProvider)
	if !ok {
		return "", nil
	}
	variant, err := resolver.FlagVariant(ctx, flag, distinctID, groups)
	if err != nil {
		return "", fmt.Errorf("resolve feature flag variant %q: %w", flag, err)
	}
	return variant, nil
}

// Evaluation reports whether a flag provider reached an authoritative decision.
// It is intentionally separate from Provider so existing feature checks can keep
// their bool-only contract while safety-sensitive callers can distinguish an
// explicit disabled result from an unavailable evaluator.
type Evaluation uint8

const (
	EvaluationIndeterminate Evaluation = iota
	EvaluationDisabled
	EvaluationEnabled
)

// EvaluationProvider is implemented by providers that can distinguish an
// unavailable or inconclusive lookup from an explicit flag result.
type EvaluationProvider interface {
	EvaluateFlag(ctx context.Context, flag Flag, distinctID string, groups map[string]string) (Evaluation, error)
}

// EvaluateFlag returns an indeterminate result for providers that only expose
// the legacy bool contract. Callers that need a fail-safe carry-forward decision
// must not treat that legacy false as an explicit disable.
func EvaluateFlag(ctx context.Context, provider Provider, flag Flag, distinctID string, groups map[string]string) (Evaluation, error) {
	if provider == nil {
		return EvaluationIndeterminate, nil
	}
	if evaluator, ok := provider.(EvaluationProvider); ok {
		evaluation, err := evaluator.EvaluateFlag(ctx, flag, distinctID, groups)
		if err != nil {
			return EvaluationIndeterminate, fmt.Errorf("evaluate feature flag %q: %w", flag, err)
		}
		return evaluation, nil
	}
	return EvaluationIndeterminate, nil
}

func (imp *InMemory) EvaluateFlag(_ context.Context, flag Flag, distinctID string, _ map[string]string) (Evaluation, error) {
	key := distinctID + ":" + string(flag)
	value, ok := (*sync.Map)(imp).Load(key)
	if !ok {
		return EvaluationIndeterminate, nil
	}
	enabled, ok := value.(bool)
	if !ok {
		return EvaluationIndeterminate, nil
	}
	if enabled {
		return EvaluationEnabled, nil
	}
	return EvaluationDisabled, nil
}

// OrgProjectGroups returns the PostHog group memberships used to evaluate
// org/project-scoped flags. It keys the "organization" group by the org slug
// and the "slug" group by "<orgSlug>/<projectSlug>" — the same group types the
// dashboard (client/dashboard/src/contexts/Telemetry.tsx) and backend event
// capture (server/internal/thirdparty/posthog) register. PostHog caps a project
// at 5 group types and these are the only org/project ones that exist; any
// other group type is silently dropped at ingestion, so a flag release targeting
// it could never match. Empty slug components are omitted. Returns nil when no
// group can be built.
func OrgProjectGroups(orgSlug, projectSlug string) map[string]string {
	if orgSlug == "" {
		return nil
	}

	groups := map[string]string{"organization": orgSlug}
	if projectSlug != "" {
		groups["slug"] = orgSlug + "/" + projectSlug
	}

	return groups
}
