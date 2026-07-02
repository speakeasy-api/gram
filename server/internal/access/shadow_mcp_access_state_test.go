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
		name         string
		rules        func(t *testing.T, ctx context.Context, ti *testInstance, orgID string, projectID string)
		rawURL       string
		wantAccess   string
		wantRuleName string
	}{
		{
			name: "exact project full url allow sets access state",
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
			rawURL:       "https://mcp.example.com/mcp?token=secret",
			wantAccess:   shadowMCPInventoryAccessAllowed,
			wantRuleName: "Project allow",
		},
		{
			name: "exact project full url deny sets access state",
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
			rawURL:       "https://mcp.example.com/mcp",
			wantAccess:   shadowMCPInventoryAccessDenied,
			wantRuleName: "Project deny",
		},
		{
			name: "host rule does not set inventory url access state",
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
			rawURL:     "https://mcp.example.com/mcp",
			wantAccess: shadowMCPInventoryAccessNone,
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
			rawURL:     "https://mcp.example.com/mcp",
			wantAccess: shadowMCPInventoryAccessNone,
		},
		{
			name: "server identity rule does not set inventory url access state",
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
			rawURL:     "https://mcp.example.com/mcp",
			wantAccess: shadowMCPInventoryAccessNone,
		},
		{
			name: "host rule does not override matching full url allow",
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
			rawURL:       "https://mcp.example.com/mcp",
			wantAccess:   shadowMCPInventoryAccessAllowed,
			wantRuleName: "Project allow",
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
			require.Equal(t, tt.wantAccess, got.Access)
			if tt.wantRuleName == "" {
				require.Nil(t, got.Rule)
			} else {
				require.NotNil(t, got.Rule)
				require.Equal(t, tt.wantRuleName, got.Rule.DisplayName)
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
