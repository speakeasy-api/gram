package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateRiskPolicy_ShadowMCPAutoNameIsFixed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)
	require.Equal(t, "Shadow MCP Server Policy", created.Name)

	// A second auto-named policy gets a numeric suffix instead of a collision.
	// Disabled: projects allow at most one enabled shadow MCP blocking policy.
	second, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		Enabled:              new(false),
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)
	require.Equal(t, "Shadow MCP Server Policy 2", second.Name)
}

func TestCreateRiskPolicy_ShadowMCPDispositionAllowAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Allow All Shadow MCP"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.ShadowMcpDisposition)
	require.Equal(t, "allow_all", *created.ShadowMcpDisposition)

	fetched, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.NotNil(t, fetched.ShadowMcpDisposition)
	require.Equal(t, "allow_all", *fetched.ShadowMcpDisposition)
}

func TestCreateRiskPolicy_ShadowMCPDispositionDefaultsToBlockAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Default Disposition Shadow MCP"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)
	require.NotNil(t, created.ShadowMcpDisposition)
	require.Equal(t, "block_all", *created.ShadowMcpDisposition)
}

func TestCreateRiskPolicy_ShadowMCPDispositionExplicitBlockAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Explicit Block All Shadow MCP"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("block_all"),
	})
	require.NoError(t, err)
	require.NotNil(t, created.ShadowMcpDisposition)
	require.Equal(t, "block_all", *created.ShadowMcpDisposition)
}

func TestCreateRiskPolicy_ShadowMCPDispositionRejectsNonShadowMCPSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	name := "Gitleaks With Disposition"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"gitleaks"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_ShadowMCPDispositionRejectsNonBlockAction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	name := "Flag Shadow MCP With Disposition"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "flag",
		ShadowMcpDisposition: new("allow_all"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_NonShadowMCPPolicyOmitsDisposition(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Plain Gitleaks"),
		Sources: []string{"gitleaks"},
	})
	require.NoError(t, err)
	require.Nil(t, created.ShadowMcpDisposition)
}

func TestUpdateRiskPolicy_ShadowMCPDispositionImmutable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Immutable Disposition"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpDisposition: new("block_all"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)

	fetched, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.NotNil(t, fetched.ShadowMcpDisposition)
	require.Equal(t, "allow_all", *fetched.ShadowMcpDisposition)
}

func TestUpdateRiskPolicy_ShadowMCPDispositionSameValueAccepted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Same Value Disposition"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.ShadowMcpDisposition)
	require.Equal(t, "allow_all", *updated.ShadowMcpDisposition)
}

func TestUpdateRiskPolicy_ShadowMCPDispositionOmittedPreserved(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Preserved Disposition"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Renamed Preserved Disposition",
	})
	require.NoError(t, err)
	require.NotNil(t, updated.ShadowMcpDisposition)
	require.Equal(t, "allow_all", *updated.ShadowMcpDisposition)
}

func TestUpdateRiskPolicy_ShadowMCPLegacyPolicyAcceptsBlockAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	// A policy created without an explicit disposition is block_all; sending
	// block_all back on update matches the effective value and passes, while
	// allow_all is a posture switch and is rejected.
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Legacy Disposition"),
		Sources: []string{"shadow_mcp"},
		Action:  "block",
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpDisposition: new("block_all"),
	})
	require.NoError(t, err)
	require.NotNil(t, updated.ShadowMcpDisposition)
	require.Equal(t, "block_all", *updated.ShadowMcpDisposition)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpDisposition: new("allow_all"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestUpdateRiskPolicy_ShadowMCPDispositionRejectedOnNonShadowPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Gitleaks No Disposition"),
		Sources: []string{"gitleaks"},
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:                   created.ID,
		Name:                 created.Name,
		ShadowMcpDisposition: new("block_all"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestUpdateRiskPolicy_ShadowMCPAllowAllBlocksSourceChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	// An allow_all policy is its blocked-server list: outside the blocking
	// posture the list has nothing to deny, so the edit is refused and the
	// rejection says which part of it was the problem.
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Morph Away Disposition"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
		ShadowMcpBlockedUrls: []string{"https://sketchy.example.com/mcp"},
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    created.Name,
		Sources: []string{"gitleaks"},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "turning off shadow_mcp detection")
	require.Contains(t, oopsErr.Error(), "Delete this policy and create the one you want instead")

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:     created.ID,
		Name:   created.Name,
		Action: new("flag"),
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), `changing the action from "block" to "flag"`)

	fetched, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.NotNil(t, fetched.ShadowMcpDisposition)
	require.Equal(t, "allow_all", *fetched.ShadowMcpDisposition)
	require.Equal(t, []string{"https://sketchy.example.com/mcp"}, shadowMCPPolicyBlockedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPStoredBlockAllRetiresOnActionChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	// Every shadow MCP policy the dashboard creates stores block_all, so the
	// stored value must not make a policy harder to edit than the legacy
	// policies that predate the column. block_all's URL list is a set of
	// exceptions to a deny, and it retires along with the deny.
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Stored Block All"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("block_all"),
		ShadowMcpAllowedUrls: []string{"https://allowed.example.com/mcp"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"https://allowed.example.com/mcp"}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:     created.ID,
		Name:   created.Name,
		Action: new("warn"),
	})
	require.NoError(t, err)
	require.Equal(t, "warn", updated.Action)
	require.Nil(t, updated.ShadowMcpDisposition)
	// The caller sent no URL list: the server retires the bypass grants itself
	// rather than leave them to reappear the next time the policy denies.
	require.Empty(t, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestUpdateRiskPolicy_ShadowMCPStoredBlockAllRetiresOnSourceChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Stored Block All Source Swap"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("block_all"),
		ShadowMcpAllowedUrls: []string{"https://allowed.example.com/mcp"},
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    created.Name,
		Sources: []string{"gitleaks"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gitleaks"}, updated.Sources)
	require.Empty(t, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}
