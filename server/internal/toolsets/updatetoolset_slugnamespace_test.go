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
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// TestToolsetsService_UpdateToolset_McpSlugTakenByEndpoint covers the
// endpoint-to-toolset direction of the unified slug namespace: an mcp_slug
// held by a live mcp_endpoints row in the platform scope cannot be claimed by
// a toolset.
func TestToolsetsService_UpdateToolset_McpSlugTakenByEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Slug Namespace Toolset",
		Description:            new("toolset colliding with an endpoint"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	other, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Wrapped Toolset",
		Description:            new("backs the endpoint holding the slug"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	takenSlug := authCtx.OrganizationSlug + "-endpoint-owned"

	wrapperID, err := uuid.NewV7()
	require.NoError(t, err)
	wrapper, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  wrapperID,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText("wrapped hosted server"),
		Slug:                conv.ToPGText("wrapped-hosted-" + uuid.NewString()),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:           uuid.NullUUID{UUID: uuid.MustParse(other.ID), Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)

	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: wrapper.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            takenSlug,
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                conv.PtrEmpty(types.Slug(takenSlug)),
		McpIsPublic:            nil,
		McpEnabled:             nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
}

// TestToolsetsService_UpdateToolset_McpSlugRenameUnderSoftDeletedDomain pins
// that a toolset still bound to a soft-deleted custom domain can rename its
// mcp_slug: the availability probe skips the domain liveness guard for the
// toolset's own scope.
func TestToolsetsService_UpdateToolset_McpSlugRenameUnderSoftDeletedDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Dead Domain Toolset",
		Description:            new("bound to a soft-deleted custom domain"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	domain, err := cdrepo.New(ti.conn).CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "dead.example.com",
		IngressName:     conv.ToPGText("ingress-dead"),
		CertSecretName:  conv.ToPGText("cert-dead"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetCustomDomain(ctx, toolsetsrepo.SetToolsetCustomDomainParams{
		CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		Slug:           conv.ToLower(string(created.Slug)),
		ProjectID:      *authCtx.ProjectID,
	}))

	_, err = ti.service.UpdateToolset(ctx, updateMcpSlugPayload(created.Slug, "dead-domain-old"))
	require.NoError(t, err)

	require.NoError(t, cdrepo.New(ti.conn).DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID))

	updated, err := ti.service.UpdateToolset(ctx, updateMcpSlugPayload(created.Slug, "dead-domain-new"))
	require.NoError(t, err)
	require.NotNil(t, updated.McpSlug)
	require.Equal(t, "dead-domain-new", string(*updated.McpSlug))
}

// TestToolsetsService_UpdateToolset_DomainChangeProbesExistingSlug pins that
// binding a toolset to a custom domain re-validates its existing mcp_slug in
// the new scope.
func TestToolsetsService_UpdateToolset_DomainChangeProbesExistingSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	ctx = withAccountType(t, ctx, "pro")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Domain Move Toolset",
		Description:            new("moves under a custom domain"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	other, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Domain Endpoint Toolset",
		Description:            new("backs the endpoint holding the slug"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	domain, err := cdrepo.New(ti.conn).CreateCustomDomain(ctx, cdrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          "move.example.com",
		IngressName:     conv.ToPGText("ingress-move"),
		CertSecretName:  conv.ToPGText("cert-move"),
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	_, err = cdrepo.New(ti.conn).SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	activated, err := cdrepo.New(ti.conn).ActivateVerifiedCustomDomain(ctx, cdrepo.ActivateVerifiedCustomDomainParams{
		IngressName:     conv.ToPGText("ingress-move"),
		CertSecretName:  conv.ToPGText("cert-move"),
		ProvisionerKind: "ingress",
		ID:              domain.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, activated)

	sharedSlug := authCtx.OrganizationSlug + "-shared-address"

	wrapperID, err := uuid.NewV7()
	require.NoError(t, err)
	wrapper, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  wrapperID,
		ProjectID:           *authCtx.ProjectID,
		Name:                conv.ToPGText("domain hosted server"),
		Slug:                conv.ToPGText("domain-hosted-" + uuid.NewString()),
		EnvironmentID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:           uuid.NullUUID{UUID: uuid.MustParse(other.ID), Valid: true},
		Visibility:          "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID:     uuid.NullUUID{UUID: wrapper.ID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            sharedSlug,
	})
	require.NoError(t, err)

	// Same slug on the platform scope is a separate namespace and succeeds.
	_, err = ti.service.UpdateToolset(ctx, updateMcpSlugPayload(created.Slug, sharedSlug))
	require.NoError(t, err)

	// Moving under the domain re-scopes the slug onto the endpoint's address.
	_, err = ti.service.UpdateToolset(ctx, &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                nil,
		McpIsPublic:            nil,
		McpEnabled:             nil,
		CustomDomainID:         conv.PtrEmpty(domain.ID.String()),
		ProjectSlugInput:       nil,
	})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeConflict, shareable.Code)
}

func updateMcpSlugPayload(slug types.Slug, mcpSlug string) *gen.UpdateToolsetPayload {
	return &gen.UpdateToolsetPayload{
		SessionToken:           nil,
		Slug:                   slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		PromptTemplateNames:    nil,
		McpSlug:                conv.PtrEmpty(types.Slug(mcpSlug)),
		McpIsPublic:            nil,
		McpEnabled:             nil,
		CustomDomainID:         nil,
		ProjectSlugInput:       nil,
	}
}
