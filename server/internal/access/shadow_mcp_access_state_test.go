package access

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestResolveShadowMCPInventoryAccessState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		rules               func(t *testing.T, ctx context.Context, ti *testInstance, orgID string, projectID string)
		rawURL              string
		wantExplicit        string
		wantEffective       string
		wantEffectiveRule   string
		wantExplanatoryRule string
	}{
		{
			name: "exact project full url allow is explicit and effective",
			rules: func(t *testing.T, ctx context.Context, ti *testInstance, orgID string, projectID string) {
				t.Helper()

				createShadowMCPInventoryAccessStateRule(t, ctx, ti, accesscontrol.AccessRule{
					OrganizationID: orgID,
					ProjectID:      projectID,
					AccessScope:    accesscontrol.AccessScopeProject,
					Disposition:    accesscontrol.DispositionAllowed,
					MatchKind:      accesscontrol.MatchKindFullURL,
					MatchValue:     "https://mcp.example.com/mcp",
					DisplayName:    "Project allow",
					ObservedSummary: accesscontrol.ObservedSummary{
						FullURL: new("https://mcp.example.com/mcp"),
					},
				})
			},
			rawURL:            "https://mcp.example.com/mcp?token=secret",
			wantExplicit:      shadowMCPInventoryAccessAllowed,
			wantEffective:     shadowMCPInventoryAccessAllowed,
			wantEffectiveRule: "Project allow",
		},
		{
			name: "exact project full url deny is explicit and effective",
			rules: func(t *testing.T, ctx context.Context, ti *testInstance, orgID string, projectID string) {
				t.Helper()

				createShadowMCPInventoryAccessStateRule(t, ctx, ti, accesscontrol.AccessRule{
					OrganizationID: orgID,
					ProjectID:      projectID,
					AccessScope:    accesscontrol.AccessScopeProject,
					Disposition:    accesscontrol.DispositionDenied,
					MatchKind:      accesscontrol.MatchKindFullURL,
					MatchValue:     "https://mcp.example.com/mcp",
					DisplayName:    "Project deny",
					ObservedSummary: accesscontrol.ObservedSummary{
						FullURL: new("https://mcp.example.com/mcp"),
					},
				})
			},
			rawURL:            "https://mcp.example.com/mcp",
			wantExplicit:      shadowMCPInventoryAccessDenied,
			wantEffective:     shadowMCPInventoryAccessDenied,
			wantEffectiveRule: "Project deny",
		},
		{
			name: "host level organization deny is effective inherited state",
			rules: func(t *testing.T, ctx context.Context, ti *testInstance, orgID string, projectID string) {
				t.Helper()

				createShadowMCPInventoryAccessStateRule(t, ctx, ti, accesscontrol.AccessRule{
					OrganizationID: orgID,
					ProjectID:      "",
					AccessScope:    accesscontrol.AccessScopeOrganization,
					Disposition:    accesscontrol.DispositionDenied,
					MatchKind:      accesscontrol.MatchKindURLHost,
					MatchValue:     "mcp.example.com",
					DisplayName:    "Org host deny",
					ObservedSummary: accesscontrol.ObservedSummary{
						URLHost: new("mcp.example.com"),
					},
				})
			},
			rawURL:            "https://mcp.example.com/mcp",
			wantExplicit:      shadowMCPInventoryAccessNone,
			wantEffective:     shadowMCPInventoryAccessDenied,
			wantEffectiveRule: "Org host deny",
		},
		{
			name: "no matching url rule returns no access state",
			rules: func(t *testing.T, ctx context.Context, ti *testInstance, orgID string, projectID string) {
				t.Helper()

				createShadowMCPInventoryAccessStateRule(t, ctx, ti, accesscontrol.AccessRule{
					OrganizationID: orgID,
					ProjectID:      projectID,
					AccessScope:    accesscontrol.AccessScopeProject,
					Disposition:    accesscontrol.DispositionAllowed,
					MatchKind:      accesscontrol.MatchKindFullURL,
					MatchValue:     "https://other.example.com/mcp",
					DisplayName:    "Other allow",
					ObservedSummary: accesscontrol.ObservedSummary{
						FullURL: new("https://other.example.com/mcp"),
					},
				})
			},
			rawURL:        "https://mcp.example.com/mcp",
			wantExplicit:  shadowMCPInventoryAccessNone,
			wantEffective: shadowMCPInventoryAccessNone,
		},
		{
			name: "server identity rule with matching observed url is explanatory only",
			rules: func(t *testing.T, ctx context.Context, ti *testInstance, orgID string, projectID string) {
				t.Helper()

				createShadowMCPInventoryAccessStateRule(t, ctx, ti, accesscontrol.AccessRule{
					OrganizationID: orgID,
					ProjectID:      projectID,
					AccessScope:    accesscontrol.AccessScopeProject,
					Disposition:    accesscontrol.DispositionAllowed,
					MatchKind:      accesscontrol.MatchKindServerIdentity,
					MatchValue:     "notion",
					DisplayName:    "Notion identity allow",
					ObservedSummary: accesscontrol.ObservedSummary{
						FullURL: new("https://mcp.example.com/mcp"),
					},
				})
			},
			rawURL:              "https://mcp.example.com/mcp",
			wantExplicit:        shadowMCPInventoryAccessNone,
			wantEffective:       shadowMCPInventoryAccessNone,
			wantExplanatoryRule: "Notion identity allow",
		},
		{
			name: "deny wins over matching allow",
			rules: func(t *testing.T, ctx context.Context, ti *testInstance, orgID string, projectID string) {
				t.Helper()

				createShadowMCPInventoryAccessStateRule(t, ctx, ti, accesscontrol.AccessRule{
					OrganizationID: orgID,
					ProjectID:      projectID,
					AccessScope:    accesscontrol.AccessScopeProject,
					Disposition:    accesscontrol.DispositionAllowed,
					MatchKind:      accesscontrol.MatchKindFullURL,
					MatchValue:     "https://mcp.example.com/mcp",
					DisplayName:    "Project allow",
					ObservedSummary: accesscontrol.ObservedSummary{
						FullURL: new("https://mcp.example.com/mcp"),
					},
				})
				createShadowMCPInventoryAccessStateRule(t, ctx, ti, accesscontrol.AccessRule{
					OrganizationID: orgID,
					ProjectID:      "",
					AccessScope:    accesscontrol.AccessScopeOrganization,
					Disposition:    accesscontrol.DispositionDenied,
					MatchKind:      accesscontrol.MatchKindURLHost,
					MatchValue:     "mcp.example.com",
					DisplayName:    "Org host deny",
					ObservedSummary: accesscontrol.ObservedSummary{
						URLHost: new("mcp.example.com"),
					},
				})
			},
			rawURL:            "https://mcp.example.com/mcp",
			wantExplicit:      shadowMCPInventoryAccessAllowed,
			wantEffective:     shadowMCPInventoryAccessDenied,
			wantEffectiveRule: "Org host deny",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestAccessService(t)
			authCtx := testAccessAuthContext(t, ctx)
			projectID := authCtx.ProjectID.String()
			tt.rules(t, ctx, ti, authCtx.ActiveOrganizationID, projectID)

			inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(tt.rawURL)
			require.True(t, ok)

			got, err := ti.service.resolveShadowMCPInventoryAccessState(ctx, authCtx.ActiveOrganizationID, projectID, inventoryURL)
			require.NoError(t, err)
			require.Equal(t, tt.wantExplicit, got.ExplicitDisposition)
			require.Equal(t, tt.wantEffective, got.EffectiveDisposition)
			if tt.wantEffectiveRule == "" {
				require.Nil(t, got.EffectiveRule)
			} else {
				require.NotNil(t, got.EffectiveRule)
				require.Equal(t, tt.wantEffectiveRule, got.EffectiveRule.DisplayName)
			}
			if tt.wantExplanatoryRule == "" {
				require.Empty(t, got.ExplanatoryRules)
			} else {
				require.Len(t, got.ExplanatoryRules, 1)
				require.Equal(t, tt.wantExplanatoryRule, got.ExplanatoryRules[0].DisplayName)
			}
		})
	}
}

func createShadowMCPInventoryAccessStateRule(t *testing.T, ctx context.Context, ti *testInstance, rule accesscontrol.AccessRule) accesscontrol.AccessRule {
	t.Helper()

	now := time.Now().UTC()
	if rule.ID == "" {
		rule.ID = uuid.NewString()
	}
	if rule.ResourceType == "" {
		rule.ResourceType = accesscontrol.ResourceTypeShadowMCP
	}
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	if rule.UpdatedAt.IsZero() {
		rule.UpdatedAt = now
	}
	created, err := ti.service.accessStore.CreateRule(ctx, rule)
	require.NoError(t, err)
	return created
}
