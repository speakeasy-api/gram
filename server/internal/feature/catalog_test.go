package feature_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

func TestGramMCPCatalogEnabled(t *testing.T) {
	t.Parallel()

	evaluationErr := errors.New("flag evaluation unavailable")
	for _, tt := range []struct {
		name    string
		enabled bool
		err     error
		want    bool
	}{
		{name: "enabled", enabled: true, want: true},
		{name: "disabled"},
		{name: "evaluation error", err: evaluationErr},
		{name: "enabled with error fails closed", enabled: true, err: evaluationErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.WithValue(t.Context(), catalogContextKey{}, new(int))
			provider := &catalogProvider{enabled: tt.enabled, err: tt.err}
			for range 2 {
				enabled, err := feature.GramMCPCatalogEnabled(ctx, provider, "test-org", "org-slug")
				require.Equal(t, tt.want, enabled)
				require.ErrorIs(t, err, tt.err)
				require.Same(t, ctx.Value(catalogContextKey{}), provider.contextValue)
				require.Equal(t, feature.Flag("gram-mcp-catalog"), provider.flag)
				require.Equal(t, "test-org", provider.distinctID)
				require.Equal(t, map[string]string{"organization": "org-slug"}, provider.groups)
			}
		})
	}
}

func TestGramMCPCatalogEnabledUnknown(t *testing.T) {
	t.Parallel()
	for _, provider := range []feature.Provider{nil, &feature.InMemory{}} {
		enabled, err := feature.GramMCPCatalogEnabled(t.Context(), provider, "test-org", "org-slug")
		require.NoError(t, err)
		require.False(t, enabled)
	}
}

func TestGramMCPCatalogEnabledEmptyOrganization(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, organizationID, organizationSlug string
	}{
		{name: "both empty"},
		{name: "empty slug", organizationID: "test-org"},
		{name: "empty ID", organizationSlug: "org-slug"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			provider := &catalogProvider{enabled: true}
			enabled, err := feature.GramMCPCatalogEnabled(t.Context(), provider, tt.organizationID, tt.organizationSlug)
			require.NoError(t, err)
			require.False(t, enabled)
			require.Zero(t, provider.calls, "empty organization must not evaluate a global rollout")
		})
	}
}

func TestGramMCPCatalogEnabledOrganizationIsolation(t *testing.T) {
	t.Parallel()
	provider := &feature.InMemory{}
	provider.SetFlag(feature.FlagGramMCPCatalog, "enabled-org", true)
	for _, organizationID := range []string{"enabled-org", "other-org"} {
		enabled, err := feature.GramMCPCatalogEnabled(t.Context(), provider, organizationID, organizationID+"-slug")
		require.NoError(t, err)
		require.Equal(t, organizationID == "enabled-org", enabled)
	}
}

type catalogContextKey struct{}

type catalogProvider struct {
	feature.Provider
	enabled      bool
	err          error
	calls        int
	contextValue *int
	flag         feature.Flag
	distinctID   string
	groups       map[string]string
}

func (p *catalogProvider) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	p.calls++
	p.contextValue, _ = ctx.Value(catalogContextKey{}).(*int)
	p.flag, p.distinctID, p.groups = flag, distinctID, groups
	return p.enabled, p.err
}
