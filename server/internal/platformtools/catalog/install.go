package catalog

import (
	"context"
	"fmt"
	"io"
	"strings"

	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	deploymentsgen "github.com/speakeasy-api/gram/server/gen/deployments"
	toolsetsgen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentsrepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcprepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const handlerInstall = "install_catalog_server"

type installInput struct {
	RegistryID        string  `json:"registry_id" jsonschema:"ID of the registry returned by platform_search_catalog."`
	RegistrySpecifier string  `json:"registry_specifier" jsonschema:"The server's registry_specifier returned by platform_search_catalog (e.g. 'io.github.user/server')."`
	Name              *string `json:"name,omitempty" jsonschema:"Optional human-readable name for the resulting toolset. Defaults to the server's title or specifier."`
}

type installResult struct {
	ToolsetID    string `json:"toolset_id"`
	ToolsetSlug  string `json:"toolset_slug"`
	ToolsetName  string `json:"toolset_name"`
	MCPSlug      string `json:"mcp_slug,omitempty"`
	DeploymentID string `json:"deployment_id"`
	Status       string `json:"status"`
}

// InstallTool wraps the catalog install flow (evolveDeployment + createToolset
// + enable MCP) used by the dashboard's AddServerDialog so an assistant can
// invoke it directly.
type InstallTool struct {
	descriptor      core.ToolDescriptor
	registryClient  *externalmcp.RegistryClient
	repo            *externalmcprepo.Queries
	deploymentsRepo *deploymentsrepo.Queries
	installer       Installer
}

func NewInstallTool(installer Installer, registryClient *externalmcp.RegistryClient, repo *externalmcprepo.Queries, deploymentsRepo *deploymentsrepo.Queries) *InstallTool {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := false

	return &InstallTool{
		installer:       installer,
		registryClient:  registryClient,
		repo:            repo,
		deploymentsRepo: deploymentsRepo,
		descriptor: core.ToolDescriptor{
			SourceSlug:  SourceCatalog,
			HandlerName: handlerInstall,
			Name:        ToolNameInstallCatalogTool,
			Description: "Install an MCP catalog server into the caller's project. Creates a new toolset wired to the server's tools and enables the toolset for MCP. Use platform_search_catalog first to discover the registry_id and registry_specifier.",
			InputSchema: core.BuildInputSchema[installInput](
				core.WithPropertyFormat("registry_id", "uuid"),
			),
			Variables:   nil,
			Annotations: catalogToolAnnotations(readOnly, destructive, idempotent, openWorld),
			Managed:     true,
			OwnerKind:   nil,
			OwnerID:     nil,
		},
	}
}

func (t *InstallTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *InstallTool) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.installer == nil || t.registryClient == nil || t.repo == nil || t.deploymentsRepo == nil {
		return oops.E(oops.CodeUnexpected, nil, "catalog tools are not configured")
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.E(oops.CodeUnauthorized, nil, "catalog tools require a project auth context")
	}

	var input installInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	registryID, err := uuid.Parse(strings.TrimSpace(input.RegistryID))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid registry_id")
	}
	specifier := strings.TrimSpace(input.RegistrySpecifier)
	if specifier == "" {
		return oops.E(oops.CodeBadRequest, nil, "registry_specifier is required")
	}

	registry, err := t.repo.GetMCPRegistryByID(ctx, registryID)
	if err != nil {
		return fmt.Errorf("get mcp registry: %w", err)
	}

	// Fetch tool + remote details so we can build the same tool URN list the
	// dashboard would build (one URN per tool, falling back to :proxy when
	// the server's tools are not known yet).
	details, err := t.registryClient.GetServerDetails(ctx, externalmcp.Registry{
		ID:  registry.ID,
		URL: registry.Url,
	}, specifier, nil)
	if err != nil {
		return fmt.Errorf("fetch catalog server details: %w", err)
	}

	displayName := strings.TrimSpace(conv.PtrValOrEmpty(input.Name, ""))
	if displayName == "" {
		displayName = defaultDisplayName(specifier, details.Name)
	}

	slug := slugFromName(displayName)
	if slug == "" {
		return oops.E(oops.CodeBadRequest, nil, "name does not produce a usable slug — pick a name with at least one alphanumeric character")
	}

	// Guard against silently clobbering an unrelated server. The deployments
	// upsert is keyed by (deployment_id, slug) and updates registry/specifier
	// on conflict, so installing "io.bar/server" when "io.foo/server" is
	// already attached under the same trailing-segment slug would replace
	// the existing attachment in the cloned deployment. The dashboard
	// equivalent is the slug-collision check in
	// useExternalMcpReleaseWorkflow.startDeployment.
	if err := t.ensureNoConflictingSlug(ctx, *authCtx.ProjectID, slug, specifier); err != nil {
		return err
	}

	regIDStr := registryID.String()
	registrySpecifier := specifier
	evolvePayload := &deploymentsgen.EvolvePayload{
		ApikeyToken:           nil,
		SessionToken:          nil,
		ProjectSlugInput:      nil,
		DeploymentID:          nil,
		NonBlocking:           nil,
		UpsertOpenapiv3Assets: nil,
		UpsertPackages:        nil,
		UpsertFunctions:       nil,
		UpsertExternalMcps: []*deploymentsgen.AddExternalMCPForm{{
			RegistryID:                          &regIDStr,
			OrganizationMcpCollectionRegistryID: nil,
			Name:                                displayName,
			Slug:                                types.Slug(slug),
			RegistryServerSpecifier:             registrySpecifier,
			SelectedRemotes:                     nil,
		}},
		ExcludeOpenapiv3Assets: nil,
		ExcludePackages:        nil,
		ExcludeFunctions:       nil,
		ExcludeExternalMcps:    nil,
	}

	evolveResult, err := t.installer.Evolve(ctx, evolvePayload)
	if err != nil {
		return fmt.Errorf("evolve deployment with catalog server: %w", err)
	}
	if evolveResult == nil || evolveResult.Deployment == nil {
		return oops.E(oops.CodeUnexpected, nil, "evolve deployment returned no deployment")
	}

	toolURNs := make([]string, 0, len(details.Tools))
	for _, tool := range details.Tools {
		if tool.Name == "" {
			continue
		}
		toolURNs = append(toolURNs, fmt.Sprintf("tools:externalmcp:%s:%s", slug, tool.Name))
	}
	if len(toolURNs) == 0 {
		toolURNs = []string{fmt.Sprintf("tools:externalmcp:%s:proxy", slug)}
	}
	description := details.Description
	if description == "" {
		description = fmt.Sprintf("MCP server: %s", specifier)
	}

	created, err := t.installer.CreateToolset(ctx, &toolsetsgen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Name:                   displayName,
		Description:            &description,
		ToolUrns:               toolURNs,
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		Origin:                 &types.ToolsetOrigin{RegistrySpecifier: specifier},
		ProjectSlugInput:       nil,
	})
	if err != nil {
		return fmt.Errorf("create toolset for catalog server: %w", err)
	}
	if created == nil {
		return oops.E(oops.CodeUnexpected, nil, "create toolset returned no toolset")
	}

	mcpEnabled := true
	mcpPublic := true
	updated, err := t.installer.UpdateToolset(ctx, &toolsetsgen.UpdateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Slug:                   created.Slug,
		Name:                   nil,
		Description:            nil,
		DefaultEnvironmentSlug: nil,
		PromptTemplateNames:    nil,
		ToolUrns:               nil,
		ResourceUrns:           nil,
		McpEnabled:             &mcpEnabled,
		McpSlug:                nil,
		McpIsPublic:            &mcpPublic,
		CustomDomainID:         nil,
		ToolSelectionMode:      nil,
		ProjectSlugInput:       nil,
	})
	if err != nil {
		return fmt.Errorf("enable mcp on toolset: %w", err)
	}

	mcpSlug := ""
	if updated != nil && updated.McpSlug != nil {
		mcpSlug = string(*updated.McpSlug)
	}

	return writeJSON(wr, installResult{
		ToolsetID:    created.ID,
		ToolsetSlug:  string(created.Slug),
		ToolsetName:  created.Name,
		MCPSlug:      mcpSlug,
		DeploymentID: evolveResult.Deployment.ID,
		Status:       evolveResult.Deployment.Status,
	})
}

func defaultDisplayName(specifier string, fallback string) string {
	specifier = strings.TrimSpace(specifier)
	if specifier == "" {
		return strings.TrimSpace(fallback)
	}
	if idx := strings.LastIndex(specifier, "/"); idx >= 0 && idx < len(specifier)-1 {
		return specifier[idx+1:]
	}
	return specifier
}

// ensureNoConflictingSlug returns a CodeConflict error when the project's
// latest deployment already attaches an external MCP under the same slug but
// for a different registry_server_specifier. Same specifier + same slug is
// a legitimate idempotent re-install and proceeds silently.
func (t *InstallTool) ensureNoConflictingSlug(ctx context.Context, projectID uuid.UUID, slug, specifier string) error {
	latestID, err := t.deploymentsRepo.GetLatestDeploymentID(ctx, projectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("get latest deployment for slug check: %w", err)
	}
	if latestID == uuid.Nil {
		return nil
	}

	attachments, err := t.repo.ListExternalMCPAttachments(ctx, latestID)
	if err != nil {
		return fmt.Errorf("list external mcp attachments for slug check: %w", err)
	}
	for _, attachment := range attachments {
		if attachment.Slug != slug {
			continue
		}
		if attachment.RegistryServerSpecifier == specifier {
			return nil
		}
		return oops.E(
			oops.CodeConflict, nil,
			"a different MCP server (%s) is already installed in this project as %q. Re-run with a name argument to disambiguate.",
			attachment.RegistryServerSpecifier, slug,
		)
	}
	return nil
}

// slugFromName mirrors the dashboard's generateSlug(name):
// take the last "/" segment, lowercase, replace non-alphanumeric runs with a
// single hyphen, trim hyphens. Kept in sync with
// client/dashboard/src/pages/catalog/useExternalMcpReleaseWorkflow.ts so the
// attachment row created here resolves the same tool URNs the UI would have
// produced.
func slugFromName(name string) string {
	lastPart := name
	if idx := strings.LastIndex(name, "/"); idx >= 0 && idx < len(name)-1 {
		lastPart = name[idx+1:]
	}
	return conv.URLToSlug(lastPart)
}
