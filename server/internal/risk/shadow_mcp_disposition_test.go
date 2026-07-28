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

func TestUpdateRiskPolicy_ShadowMCPExplicitDispositionBlocksSourceChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)

	// A policy with an explicitly stored disposition cannot morph away from
	// being a blocking shadow MCP policy — that would silently drop the
	// posture (and orphan any blocked-URL list).
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 new("Morph Away Disposition"),
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpDisposition: new("allow_all"),
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

	_, err = ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:     created.ID,
		Name:   created.Name,
		Action: new("flag"),
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)

	fetched, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.NotNil(t, fetched.ShadowMcpDisposition)
	require.Equal(t, "allow_all", *fetched.ShadowMcpDisposition)
}
