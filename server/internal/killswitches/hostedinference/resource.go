package hostedinference

import (
	"context"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/killswitches"
)

// ResourceAdapter owns the static canonical identities for the enumerated
// governed Gram-hosted inference categories.
type ResourceAdapter struct{}

var _ killswitches.ResourceAdapter = ResourceAdapter{}

func (ResourceAdapter) Kind() killswitches.ResourceKind { return ResourceKindGramHostedInference }

func (ResourceAdapter) Canonicalize(_ killswitches.OrganizationID, input string) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	category := CallCategory(strings.TrimSpace(input))
	if !isGovernedCategory(category) {
		return killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey](), nil
	}
	result, err := killswitches.NewCanonicalizationResult(killswitches.ResourceKey(category))
	if err != nil {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("canonicalize hosted-inference resource: %w", err)
	}
	return result, nil
}

func (a ResourceAdapter) ValidateCurrentOrganization(_ context.Context, organizationID killswitches.OrganizationID, key killswitches.ResourceKey) (bool, error) {
	if organizationID == "" {
		return false, nil
	}
	result, err := a.Canonicalize(organizationID, string(key))
	if err != nil {
		return false, err
	}
	canonical, supported, err := result.Key()
	return supported && canonical == key, err
}

func (a ResourceAdapter) Derive(_ context.Context, organizationID killswitches.OrganizationID, source any) (killswitches.CanonicalizationResult[killswitches.ResourceKey], error) {
	category, ok := source.(CallCategory)
	if !ok {
		return killswitches.CanonicalizationResult[killswitches.ResourceKey]{}, fmt.Errorf("unsupported hosted-inference resource source type %T", source)
	}
	return a.Canonicalize(organizationID, string(category))
}

func ResourceFixtures() []killswitches.ResourceCanonicalizationFixture {
	result := make([]killswitches.ResourceCanonicalizationFixture, 0, 6)
	for category, class := range categoryClasses {
		if class != CallClassGovernedUser {
			continue
		}
		expected, _ := killswitches.NewCanonicalizationResult(killswitches.ResourceKey(category))
		result = append(result, killswitches.ResourceCanonicalizationFixture{OrganizationID: "org_fixture", Input: string(category), Expected: expected})
	}
	result = append(result, killswitches.ResourceCanonicalizationFixture{OrganizationID: "org_fixture", Input: "unknown", Expected: killswitches.UnsupportedCanonicalizationResult[killswitches.ResourceKey]()})
	return result
}
