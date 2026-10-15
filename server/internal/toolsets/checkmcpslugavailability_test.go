package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	cdrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// The deprecated check answers "taken in any scope", so it cannot delegate to
// the scoped endpoint check: a slug held only under a custom domain is taken
// here but free on the platform scope.
func TestToolsetsService_CheckMCPSlugAvailability_TakenInAnyScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Domain Slug Toolset",
		Description:            new("holds a slug under a custom domain"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	domain, err := cdrepo.New(ti.conn).CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "slugcheck.example.com",
		IngressName:     conv.ToPGText("ingress-slugcheck"),
		CertSecretName:  conv.ToPGText("cert-slugcheck"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetCustomDomain(ctx, toolsetsrepo.SetToolsetCustomDomainParams{
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		Slug:           conv.ToLower(string(created.Slug)),
		ProjectID:      *authCtx.ProjectID,
	}))

	heldSlug := "domain-held-" + uuid.NewString()[:8]
	_, err = ti.service.UpdateToolset(ctx, updateMcpSlugPayload(created.Slug, heldSlug))
	require.NoError(t, err)

	taken, err := ti.service.CheckMCPSlugAvailability(ctx, &gen.CheckMCPSlugAvailabilityPayload{Slug: types.Slug(heldSlug)})
	require.NoError(t, err)
	require.True(t, taken)

	platformAvailable, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:           heldSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.True(t, platformAvailable)

	unclaimed := "unclaimed-" + uuid.NewString()[:8]
	taken, err = ti.service.CheckMCPSlugAvailability(ctx, &gen.CheckMCPSlugAvailabilityPayload{Slug: types.Slug(unclaimed)})
	require.NoError(t, err)
	require.False(t, taken)

	// The endpoint check reads an unknown domain as unavailable, even for an unclaimed slug.
	foreignAvailable, err := mcpendpoints.CheckSlugAvailable(ctx, ti.conn, mcpendpoints.SlugAvailabilityCheck{
		Slug:           unclaimed,
		CustomDomainID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.False(t, foreignAvailable)
}
