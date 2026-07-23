package risk_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestUpdateRiskPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	enabled := true
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Original"),
		Sources: []string{"gitleaks"},
		Enabled: &enabled,
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Renamed",
	})
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.Equal(t, []string{"gitleaks"}, updated.Sources, "sources should be preserved")
	require.True(t, updated.Enabled, "enabled should be preserved")
	require.Equal(t, int64(1), updated.Version, "name-only change should not bump version")
}

func TestUpdateRiskPolicy_BumpsVersionOnSourcesChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Version Test"),
		Sources: []string{"gitleaks"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    "Version Test",
		Sources: []string{"gitleaks", "presidio"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.Version, "sources change should bump version")
}

func TestUpdateRiskPolicy_BumpsVersionOnEnabledChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Toggle Test"),
	})
	require.NoError(t, err)

	disabled := false
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    "Toggle Test",
		Enabled: &disabled,
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.Equal(t, int64(2), updated.Version, "enabled change should bump version")
}

func TestUpdateRiskPolicy_PreservesFieldsWhenOmitted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	disabled := false
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Preserve Test"),
		Sources: []string{"gitleaks"},
		Enabled: &disabled,
	})
	require.NoError(t, err)
	require.False(t, created.Enabled)

	// Update only name — enabled and sources should be preserved.
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "New Name",
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled, "enabled should remain false")
	require.Equal(t, []string{"gitleaks"}, updated.Sources, "sources should remain")
}

func TestUpdateRiskPolicy_ShadowMCPFlagToBlockAddsAllowedURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Flag"),
		Sources: []string{"shadow_mcp"},
		Action:  "flag",
	})
	require.NoError(t, err)

	block := "block"
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		Action:               &block,
		ShadowMcpAllowedUrls: []string{"HTTPS://GITHUB.EXAMPLE.COM:443/mcp?token=ignored"},
	})
	require.NoError(t, err)
	require.Equal(t, "block", updated.Action)
	require.Equal(t, []string{"https://github.example.com/mcp"}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPAllowedURLsRequireCurrentProjectInventory(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Inventory Update"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	observedURL := "https://github.example.com/mcp"
	lookupCalls := 0
	ti.shadowMCPInventoryURLLookup = func(_ context.Context, projectID uuid.UUID, canonicalURLs []string) ([]string, error) {
		lookupCalls++
		require.Equal(t, *authCtx.ProjectID, projectID)
		require.Equal(t, []string{observedURL}, canonicalURLs)
		return canonicalURLs, nil
	}

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpAllowedUrls: []string{"HTTPS://GITHUB.EXAMPLE.COM:443/mcp?secret=ignored"},
	})
	require.NoError(t, err)
	require.Equal(t, 1, lookupCalls)
	require.Equal(t, []string{observedURL}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPUnobservedURLRejectedBeforeMutation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Before Unobserved Update"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)
	signalsBefore := len(ti.signaler.Calls())
	invalidationsBefore := ti.cacheDeletes.DeleteCountContaining("shadow_mcp_policy_enabled")
	lookupCalls := 0
	reconcileCalls := 0
	ti.shadowMCPInventoryURLLookup = func(context.Context, uuid.UUID, []string) ([]string, error) {
		lookupCalls++
		return nil, nil
	}
	ti.reconcileShadowMCPPolicyURLs = func(context.Context, riskrepo.DBTX, policybypass.ReconcilePolicyURLsInput) error {
		reconcileCalls++
		return nil
	}

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 "Must Not Persist",
		ShadowMcpAllowedUrls: []string{"https://unobserved.example.com/mcp"},
	})
	require.ErrorContains(t, err, "has not been observed in this project")
	require.Equal(t, 1, lookupCalls)
	require.Zero(t, reconcileCalls)
	require.Len(t, ti.signaler.Calls(), signalsBefore)
	require.Equal(t, invalidationsBefore, ti.cacheDeletes.DeleteCountContaining("shadow_mcp_policy_enabled"))
	unchanged, getErr := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, getErr)
	require.Equal(t, created.Name, unchanged.Name)
	require.Empty(t, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPURLObservedOnlyByAnotherProjectRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Shadow MCP Cross Project Update"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)
	otherProjectID := uuid.New()
	ti.shadowMCPInventoryURLLookup = func(_ context.Context, projectID uuid.UUID, canonicalURLs []string) ([]string, error) {
		if projectID == otherProjectID {
			return canonicalURLs, nil
		}
		return nil, nil
	}

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpAllowedUrls: []string{"https://other-project.example.com/mcp"},
	})
	require.ErrorContains(t, err, "has not been observed in this project")
}

func TestUpdateRiskPolicy_ShadowMCPReplacesAllowedURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Shadow MCP Replace"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"https://old.example.com/mcp", "https://keep.example.com/sse"},
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpAllowedUrls: []string{"https://keep.example.com/sse", "https://new.example.com/mcp"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"https://keep.example.com/sse", "https://new.example.com/mcp"}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPPreservesAnotherPolicyGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	sharedURL := "https://shared.example.com/mcp"
	first, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Shadow MCP First"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{sharedURL},
	})
	require.NoError(t, err)
	second, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Shadow MCP Second"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{sharedURL},
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   first.ID,
		Name:                 first.Name,
		ShadowMcpAllowedUrls: []string{},
	})
	require.NoError(t, err)
	require.Empty(t, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, first.ID))
	require.Equal(t, []string{sharedURL}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, second.ID))
}

func TestUpdateRiskPolicy_ShadowMCPReplacesAllowedURLAudience(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	serverURL := "https://audience.example.com/mcp"
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Shadow MCP Audience"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{serverURL},
	})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{serverURL: {authz.AllUsersPrincipal().String()}}, shadowMCPPolicyURLPrincipals(t, ctx, ti.conn, created.ID))

	targeted := "targeted"
	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                    created.ID,
		Name:                  created.Name,
		AudienceType:          &targeted,
		AudiencePrincipalUrns: []string{"user:" + authCtx.UserID},
	})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{serverURL: {"user:" + authCtx.UserID}}, shadowMCPPolicyURLPrincipals(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPEmptyAllowedURLsClearsWhileChangingToFlag(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Shadow MCP Clear"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"https://clear.example.com/mcp"},
	})
	require.NoError(t, err)
	ti.shadowMCPInventoryURLLookup = func(context.Context, uuid.UUID, []string) ([]string, error) {
		return nil, errors.New("inventory lookup must not run for explicit clear")
	}

	flag := "flag"
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		Action:               &flag,
		ShadowMcpAllowedUrls: []string{},
	})
	require.NoError(t, err)
	require.Equal(t, "flag", updated.Action)
	require.Empty(t, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPOmittedAllowedURLsPreservesGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Shadow MCP Preserve"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"https://preserve.example.com/mcp"},
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Shadow MCP Preserve Renamed",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"https://preserve.example.com/mcp"}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPReconcileFailureRollsBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	oldURL := "https://old.example.com/mcp"
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Shadow MCP Rollback"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{oldURL},
	})
	require.NoError(t, err)

	ti.reconcileShadowMCPPolicyURLs = func(context.Context, riskrepo.DBTX, policybypass.ReconcilePolicyURLsInput) error {
		return errors.New("injected grant failure")
	}
	flag := "flag"
	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		Action:               &flag,
		ShadowMcpAllowedUrls: []string{},
	})
	require.ErrorContains(t, errors.Unwrap(err), "injected grant failure")

	unchanged, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, "block", unchanged.Action)
	require.Equal(t, []string{oldURL}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}
