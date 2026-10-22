package platformmcp

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/stretchr/testify/require"
	"net/url"
	"testing"
	"time"
)

func TestSetupResourcesOrganizationCallback(t *testing.T) {
	t.Parallel()
	legacy, err := url.Parse("https://legacy.example.test")
	require.NoError(t, err)
	canonical, err := url.Parse("https://canonical.example.test")
	require.NoError(t, err)
	resolver := orghost.New(orghost.Config{ServerURL: canonical, LegacyDefaultHost: legacy})
	for _, stored := range []pgtype.Text{{}, {String: canonical.String(), Valid: true}} {
		resource := testSetupResource()
		resource.Text = "Register callback {{organization_oauth_callback_url}}"
		resource.Body = resource.Text
		resources := WithSetupCallbackResolver([]SetupResource{resource}, "{{organization_oauth_callback_url}}", func(ctx context.Context, org string) (string, error) {
			require.Equal(t, "test-organization", org)
			return resolver.ServerURL(stored).String() + "/mcp/remote_login_callback", nil
		})
		ctx := ContextWithPrincipal(t.Context(), Principal{OrganizationID: "test-organization"})
		reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
		now := func() time.Time { return resource.RevalidateBy.Add(-time.Hour) }
		registerSetupResources(reg, resources, now)
		want := resolver.ServerURL(stored).String() + "/mcp/remote_login_callback"
		for _, audience := range []Audience{AudienceExternal, AudienceAssistant} {
			descriptor, ok := reg.ResourceFor(audience, resource.URI)
			require.True(t, ok)
			text, err := descriptor.Read(ctx)
			require.NoError(t, err)
			require.Contains(t, text, want)
		}
		hits, err := NewMemoryDocsIndex(resources, now).Search(ctx, "callback", 5)
		require.NoError(t, err)
		require.NotEmpty(t, hits)
		require.Contains(t, hits[0].Excerpt, want)
	}
}

func TestSetupResourceCallbackFailureIsSafe(t *testing.T) {
	t.Parallel()
	resource := testSetupResource()
	resource.Render = func(context.Context, string) (string, error) {
		return "", errors.New("private SQL driver detail")
	}
	reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
	registerSetupResources(reg, []SetupResource{resource}, func() time.Time { return resource.RevalidateBy.Add(-time.Hour) })
	for _, audience := range []Audience{AudienceExternal, AudienceAssistant} {
		descriptor, ok := reg.ResourceFor(audience, resource.URI)
		require.True(t, ok)
		_, err := descriptor.Read(t.Context())
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private SQL driver detail")
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, unavailableCode, refusal.Code)
	}
}
