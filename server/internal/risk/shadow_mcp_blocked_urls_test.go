package risk_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateRiskPolicy_ShadowMCPBlockedURLsPersisted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	lookupCalls := 0
	ti.shadowMCPInventoryURLLookup = func(context.Context, uuid.UUID, []string) ([]string, error) {
		lookupCalls++
		return nil, errors.New("inventory lookup must not run for blocked urls")
	}

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Allow All With Blocklist"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{
			"HTTPS://SKETCHY.EXAMPLE.COM:443/mcp?token=ignored",
			"https://bad.example.com/sse",
		},
	})
	require.NoError(t, err)
	// Blocking an unobserved server is deliberate (proactive defense), so no
	// inventory lookup runs for the blocked list.
	require.Zero(t, lookupCalls)
	require.Equal(t, []string{
		"https://bad.example.com/sse",
		"https://sketchy.example.com/mcp",
	}, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, created.ID))

	// Block rules apply project-wide: every grant is held by the all-users
	// principal, never by individual audience members.
	principals := shadowMCPPolicyURLPrincipalsForScope(t, ctx, ti.conn, authz.ScopeRiskPolicyBlock, created.ID)
	for serverURL, urns := range principals {
		require.Equal(t, []string{authz.AllUsersPrincipal().String()}, urns, "server %s", serverURL)
	}

	// The blocked list never reconciles into bypass grants.
	require.Empty(t, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestCreateRiskPolicy_ShadowMCPBlockedURLsRejectedOnBlockAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	name := "Block All With Blocklist"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpBlockedUrls: []string{"https://sketchy.example.com/mcp"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_ShadowMCPAllowedURLsRejectedOnAllowAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	ti.shadowMCPInventoryURLLookup = func(_ context.Context, _ uuid.UUID, canonicalURLs []string) ([]string, error) {
		return canonicalURLs, nil
	}

	name := "Allow All With Allowlist"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpAllowedUrls: []string{"https://github.example.com/mcp"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_ShadowMCPBlockedURLsInvalidURLRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	name := "Allow All Bad URL"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{"not a url"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_BlockAllPolicyOmitsBlockedURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Block All No Blocklist"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)
	require.Empty(t, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPBlockedURLsReplaced(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Replace Blocklist"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{"https://old.example.com/mcp"},
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpBlockedUrls: []string{"https://new.example.com/mcp"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"https://new.example.com/mcp"}, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, created.ID))
	// Blocklist edits do not bump the policy version: enforcement reads the
	// grants at hook time and no message re-analysis is needed.
	require.Equal(t, created.Version, updated.Version)
}

func TestUpdateRiskPolicy_ShadowMCPBlockedURLsOmittedPreserved(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Preserve Blocklist"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{"https://keep.example.com/mcp"},
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Renamed Preserve Blocklist",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"https://keep.example.com/mcp"}, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPBlockedURLsEmptyClears(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Clear Blocklist"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{"https://gone.example.com/mcp"},
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpBlockedUrls: []string{},
	})
	require.NoError(t, err)
	require.Empty(t, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPBlockedURLsRejectedOnBlockAllPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Block All Update Blocklist"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpBlockedUrls: []string{"https://sketchy.example.com/mcp"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.Empty(t, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPAllowedURLsRejectedOnAllowAllPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	ti.shadowMCPInventoryURLLookup = func(_ context.Context, _ uuid.UUID, canonicalURLs []string) ([]string, error) {
		return canonicalURLs, nil
	}

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Allow All Update Allowlist"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpAllowedUrls: []string{"https://github.example.com/mcp"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}
