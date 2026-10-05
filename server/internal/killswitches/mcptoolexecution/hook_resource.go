package mcptoolexecution

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

// HookActivityResourceAdapter canonicalizes the four code-registered hook
// checkpoints. The resource is code-owned, so current organization validation
// means only that the exact registered key remains supported; tenant identity
// is independently bound by the assertion and evaluation request.
type HookActivityResourceAdapter struct{}

// HookActivitySource is provider/event context produced after acting-user
// assertion verification. Derive intentionally rejects bare resource strings.
type HookActivitySource struct {
	Provider string
	Event    string
}

func (HookActivityResourceAdapter) Kind() killswitches.ResourceKind { return ResourceKindHookActivity }

func (HookActivityResourceAdapter) Canonicalize(_ killswitches.OrganizationID, input string) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	if !supportedHookActivity(input) {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey](), nil
	}
	result, err := killswitches.NewCanonicalizationResult(killswitches.ResourceKey(input))
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("canonicalize hook activity resource: %w", err)
	}
	return result, nil
}

func (a HookActivityResourceAdapter) ValidateCurrentOrganization(_ context.Context, organizationID killswitches.OrganizationID, key killswitches.ResourceKey) (bool, error) {
	return organizationID != "" && supportedHookActivity(string(key)), nil
}

func (a HookActivityResourceAdapter) Derive(_ context.Context, organizationID killswitches.OrganizationID, source any) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	activity, ok := source.(HookActivitySource)
	if !ok {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey](), nil
	}
	key, ok := delegation.ResourceKey(activity.Provider, activity.Event)
	if !ok {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey](), nil
	}
	return a.Canonicalize(organizationID, key)
}

func supportedHookActivity(key string) bool {
	for _, binding := range delegation.ApprovedBindings() {
		if key == binding.ResourceKey {
			return true
		}
	}
	return false
}

func hookResourceFixtures() []killswitches.ResourceCanonicalizationFixture {
	bindings := delegation.ApprovedBindings()
	result := make([]killswitches.ResourceCanonicalizationFixture, 0, len(bindings))
	for _, binding := range bindings {
		result = append(result, killswitches.ResourceCanonicalizationFixture{OrganizationID: "fixture-org", Input: binding.ResourceKey, Expected: supportedKey(killswitches.ResourceKey(binding.ResourceKey))})
	}
	return result
}
