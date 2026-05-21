package mcpendpoints_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCheckMcpEndpointSlugAvailability_PlatformDomainAvailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	available, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug("never-taken-" + uuid.NewString()[:8]),
		CustomDomainID:   nil,
	})
	require.NoError(t, err)
	require.True(t, available)
}

func TestCheckMcpEndpointSlugAvailability_PlatformDomainTaken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	slug := authCtx.OrganizationSlug + "-taken"

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)

	available, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug(slug),
		CustomDomainID:   nil,
	})
	require.NoError(t, err)
	require.False(t, available)
}

func TestCheckMcpEndpointSlugAvailability_CustomDomainAvailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "custom-" + uuid.NewString() + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	customDomainID := domain.ID.String()

	available, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug("free-on-this-domain"),
		CustomDomainID:   &customDomainID,
	})
	require.NoError(t, err)
	require.True(t, available)
}

func TestCheckMcpEndpointSlugAvailability_CustomDomainTaken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()

	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "custom-" + uuid.NewString() + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	customDomainID := domain.ID.String()
	slug := "taken-on-domain"

	_, err = ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   &customDomainID,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(slug),
	})
	require.NoError(t, err)

	available, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug(slug),
		CustomDomainID:   &customDomainID,
	})
	require.NoError(t, err)
	require.False(t, available)
}

// TestCheckMcpEndpointSlugAvailability_NamespacesAreSeparate verifies that
// taking a platform-domain slug does not affect availability of the same
// slug under a custom domain (and vice versa). The two unique-index
// namespaces are independent.
func TestCheckMcpEndpointSlugAvailability_NamespacesAreSeparate(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	mcpServerID := seedMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	platformSlug := authCtx.OrganizationSlug + "-shared"

	_, err := ti.service.CreateMcpEndpoint(ctx, &gen.CreateMcpEndpointPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		CustomDomainID:   nil,
		McpServerID:      mcpServerID,
		Slug:             types.McpEndpointSlug(platformSlug),
	})
	require.NoError(t, err)

	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Domain:         "custom-" + uuid.NewString() + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	customDomainID := domain.ID.String()

	// The same slug should still be available under the custom domain.
	available, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug(platformSlug),
		CustomDomainID:   &customDomainID,
	})
	require.NoError(t, err)
	require.True(t, available)
}

func TestCheckMcpEndpointSlugAvailability_InvalidCustomDomainID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bogus := "not-a-uuid"
	_, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug("anything"),
		CustomDomainID:   &bogus,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestCheckMcpEndpointSlugAvailability_TreatsForeignOrganizationDomainAsUnavailable
// verifies that a caller probing a custom_domain_id belonging to a different
// organization always sees "unavailable", regardless of what's actually
// registered there. The underlying CheckSlugAvailability query folds in an
// organization-ownership check so callers can't enumerate slugs under domains
// they don't own. Returning false (rather than a distinguishable error) keeps
// the response shape symmetric — the caller can't tell "domain not mine" from
// "slug taken".
func TestCheckMcpEndpointSlugAvailability_TreatsForeignOrganizationDomainAsUnavailable(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Create a custom domain under an unrelated organization ID.
	foreignOrg := "org_" + uuid.NewString()
	foreignDomain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: foreignOrg,
		Domain:         "foreign-" + uuid.NewString() + ".example.com",
		IngressName:    pgtype.Text{String: "", Valid: false},
		CertSecretName: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	foreignDomainID := foreignDomain.ID.String()

	available, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug("anything"),
		CustomDomainID:   &foreignDomainID,
	})
	require.NoError(t, err)
	require.False(t, available)
}

func TestCheckMcpEndpointSlugAvailability_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.CheckMcpEndpointSlugAvailability(ctx, &gen.CheckMcpEndpointSlugAvailabilityPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		Slug:             types.McpEndpointSlug("anything"),
		CustomDomainID:   nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
