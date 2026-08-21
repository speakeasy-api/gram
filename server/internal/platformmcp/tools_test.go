package platformmcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestOperationBudgetToolResultMapsRegistrationInputErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		code string
	}{
		{
			name: "invalid registration input",
			err:  fmt.Errorf("begin catalog registration receipt: %w", ErrRegistrationInvalid),
			code: "invalid_request",
		},
		{
			name: "registration not found",
			err:  ErrReadinessRegistrationNotFound,
			code: "registration_not_found",
		},
		{
			name: "catalog unavailable",
			err:  ErrCatalogUnavailable,
			code: unavailableCode,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, ok := operationBudgetToolResult(test.err)

			require.True(t, ok)
			require.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			var body operationBudgetResult
			require.NoError(t, json.Unmarshal([]byte(text.Text), &body))
			require.Equal(t, test.code, body.Code)
			if errors.Is(test.err, ErrCatalogUnavailable) {
				require.Equal(t, "catalog_unavailable", body.Reason)
				require.Contains(t, body.Message, "Catalogue is temporarily unavailable")
			}
		})
	}
}

// The onboarding lifecycle capability is registered from two places — live
// tools when its dependencies are wired, unavailable stubs when they are not.
// They diverged once already: the stubs predated the live tools and kept
// placeholder names (distribute_mcp_to_default_plugin) after the real ones
// arrived under the onboarding flow's vocabulary, so a client saw one name
// replace another as the rollout flipped rather than one tool change
// availability. Identity now comes from one table; these tests keep both
// registrars on it.
func TestUnavailableOnboardingToolsMirrorTheLifecycleTable(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, CatalogDescriptor{})

	registered := map[string]Descriptor{}
	for _, descriptor := range registrar.For(AudienceExternal) {
		registered[descriptor.Name] = descriptor
	}

	for _, identity := range onboardingLifecycleToolIdentities {
		descriptor, ok := registered[identity.Name]
		require.True(t, ok, "lifecycle tool %q has no unavailable stub", identity.Name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope,
			"stub %q must carry its live counterpart's project scope", identity.Name)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences,
			"stub %q must carry its live counterpart's audiences", identity.Name)
	}
}

// A lifecycle tool must never reach the assistant, in either state. The live
// tools are external-only because onboarding milestones are connection-scoped,
// and an assistant has no connection — a stub admitted where its live
// counterpart is not would advertise a capability that can only ever error.
func TestUnavailableOnboardingToolsAreNotAdmittedToTheAssistant(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, CatalogDescriptor{})

	admitted := map[string]bool{}
	for _, descriptor := range registrar.For(AudienceAssistant) {
		admitted[descriptor.Name] = true
	}

	for _, identity := range onboardingLifecycleToolIdentities {
		require.False(t, admitted[identity.Name],
			"lifecycle stub %q must not be admitted to the assistant", identity.Name)
	}
}

// The rollout changes what a tool can do, never what it is called. Composing
// the catalogue with and without the onboarding dependencies must therefore
// produce the same names on the same audiences — the unavailable stubs stand
// in for the live tools, so any name a client learns in one state is still
// there in the other.
func TestCatalogueSurfaceIsTheSameWithAndWithoutOnboardingDependencies(t *testing.T) {
	t.Parallel()

	registrations := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, &recordingRegistrationStore{})

	_, stubbed := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, CatalogDescriptor{})
	_, live := newServer(nil, testCatalog{}, registrations, "cursor-key-material", nil, nil, &OnboardingService{}, &DistributionService{}, nil, CatalogDescriptor{})

	for _, audience := range []Audience{AudienceExternal, AudienceAssistant} {
		require.Equal(t, descriptorNames(stubbed, audience), descriptorNames(live, audience),
			"audience %q sees a different tool set depending on whether onboarding dependencies are wired", audience)
	}
}

func descriptorNames(registrar *Registrar, audience Audience) []string {
	descriptors := registrar.For(audience)
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.Name)
	}
	slices.Sort(names)
	return names
}
