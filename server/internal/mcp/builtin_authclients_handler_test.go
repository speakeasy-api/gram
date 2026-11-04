package mcp_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/stretchr/testify/require"
)

func TestServeBuiltinCredentialModeNoFallback(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"legacy", "shadow", "enforced"} {
		for _, stored := range []bool{false, true} {
			name := mode + "/missing_environment"
			if stored {
				name = mode + "/stored_environment"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				ctx, ti := newTestMCPService(t)
				ac, _ := contextvalues.GetAuthContext(ctx)
				_, token := seedBuiltinAgent(t, ctx, ti, true)
				if mode == "shadow" {
					ti.features.SetFlag(feature.FlagAuthClientResolverShadow, ac.ActiveOrganizationID, true)
				}
				if mode == "enforced" {
					_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: ac.ActiveOrganizationID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
					require.NoError(t, err)
				}
				environment, err := envrepo.New(ti.conn).GetEnvironmentBySlug(ctx, envrepo.GetEnvironmentBySlugParams{Slug: "default", ProjectID: *ac.ProjectID})
				require.NoError(t, err)
				if stored {
					_, err = envrepo.New(ti.conn).UpsertEnvironmentEntry(ctx, envrepo.UpsertEnvironmentEntryParams{Name: "SLACK_BOT_TOKEN", Value: "fixture-stored-token", EnvironmentID: environment.ID, ProjectID: *ac.ProjectID})
				} else {
					_, err = envrepo.New(ti.conn).DeleteEnvironment(ctx, envrepo.DeleteEnvironmentParams{Slug: "default", ProjectID: *ac.ProjectID})
				}
				require.NoError(t, err)
				// An unknown tool avoids Slack I/O but proves credential resolution runs
				// before dispatch. Request credentials must not bypass a missing choice.
				body := makeMetaRPCBody(t, "tools/call", map[string]any{"name": "not_a_builtin_tool", "arguments": map[string]any{}})
				response, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, token, body, map[string]string{"MCP-SLACK-BOT-TOKEN": "fixture-header-token"})
				require.NoError(t, err)
				if mode == "enforced" {
					require.Contains(t, response.Body.String(), "select an available static Slack credential")
					require.NotContains(t, response.Body.String(), "tool not found")
					require.NotContains(t, response.Body.String(), "fixture-header-token")
				} else {
					require.Contains(t, response.Body.String(), "tool not found")
				}
			})
		}
	}
}
