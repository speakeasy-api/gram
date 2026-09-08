package mcpendpoints

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcp_repo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	metamcp_visibility "github.com/speakeasy-api/gram/server/internal/metamcp/visibility"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

type NamespaceKind string

const (
	NamespacePlatform     NamespaceKind = "platform"
	NamespaceCustomDomain NamespaceKind = "custom_domain"
)

// ResolutionInput is the complete addressing and policy authority for one MCP
// endpoint lookup. Callers may use FromContext for public/custom-domain
// requests. Private-listener callers must provide the ingress-pinned namespace
// and expected organization explicitly once that listener lands.
type ResolutionInput struct {
	Slug                 string
	NamespaceKind        NamespaceKind
	CustomDomainID       uuid.NullUUID
	ExpectedOrganization string
	Surface              networkaccess.Surface
}

// ResolutionResult distinguishes a genuine address miss from an authoritative
// endpoint result. An authoritative denial must terminate the request even
// though its public HTTP representation is 404; callers must not continue into
// legacy toolset fallback.
type ResolutionResult struct {
	Endpoint   *repo.McpEndpoint
	Server     *mcpservers_repo.McpServer
	MetaServer *metamcp_repo.MetaMcpServer
	Mode       networkaccess.Mode
	Found      bool
	Allowed    bool
}

// FromContext derives the current public/custom-domain namespace and surface.
// Missing request-origin context fails closed: handlers behind the production
// middleware always receive one, while direct/internal callers must stamp it.
func FromContext(ctx context.Context, slug string) (ResolutionInput, error) {
	origin, hasOrigin := requestorigin.FromContext(ctx)
	input := ResolutionInput{
		Slug:                 slug,
		NamespaceKind:        NamespacePlatform,
		CustomDomainID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExpectedOrganization: "",
		Surface:              networkaccess.SurfacePublic,
	}
	if !hasOrigin {
		// Direct internal/test callers that bypass HTTP middleware retain the
		// legacy public namespace. They cannot acquire private surface authority.
		if domainCtx := customdomains.FromContext(ctx); domainCtx != nil {
			input.NamespaceKind = NamespaceCustomDomain
			input.CustomDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
			input.ExpectedOrganization = domainCtx.OrganizationID
		}
		return input, nil
	}
	input.ExpectedOrganization = origin.OrganizationID
	switch origin.Surface {
	case requestorigin.SurfacePlatform:
	case requestorigin.SurfaceCustomDomain:
		domainCtx := customdomains.FromContext(ctx)
		if domainCtx == nil || domainCtx.DomainID == uuid.Nil || origin.OrganizationID == "" || origin.OrganizationID != domainCtx.OrganizationID {
			return ResolutionInput{}, fmt.Errorf("custom-domain request authority is incomplete")
		}
		input.NamespaceKind = NamespaceCustomDomain
		input.CustomDomainID = uuid.NullUUID{UUID: domainCtx.DomainID, Valid: true}
	case requestorigin.SurfacePrivateNetwork:
		input.Surface = networkaccess.SurfacePrivate
		return ResolutionInput{}, fmt.Errorf("private request namespace must be supplied by the private listener")
	default:
		return ResolutionInput{}, fmt.Errorf("unknown request origin surface %q", origin.Surface)
	}
	return input, nil
}

// Resolve walks the namespace-scoped addressing chain and applies visibility,
// tenant, and network-mode policy before any route-specific side effects.
func Resolve(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, input ResolutionInput) (ResolutionResult, error) {
	if input.Slug == "" {
		return ResolutionResult{}, oops.E(oops.CodeNotFound, nil, "mcp endpoint not found")
	}
	if input.Surface == networkaccess.SurfacePrivate && input.ExpectedOrganization == "" {
		return deniedResult(nil, nil, nil, networkaccess.ModePrivateOnly), nil
	}
	switch input.NamespaceKind {
	case NamespacePlatform:
		if input.CustomDomainID.Valid {
			return deniedResult(nil, nil, nil, networkaccess.ModePrivateOnly), nil
		}
	case NamespaceCustomDomain:
		if !input.CustomDomainID.Valid || input.CustomDomainID.UUID == uuid.Nil || input.ExpectedOrganization == "" {
			// A pinned custom-domain namespace whose FK was cleared must never be
			// reinterpreted as the platform namespace.
			return deniedResult(nil, nil, nil, networkaccess.ModePrivateOnly), nil
		}
	default:
		return deniedResult(nil, nil, nil, networkaccess.ModePrivateOnly), nil
	}

	endpoint, err := repo.New(db).GetMCPEndpointByCustomDomainAndSlug(ctx, repo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           input.Slug,
		CustomDomainID: input.CustomDomainID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ResolutionResult{Endpoint: nil, Server: nil, MetaServer: nil, Mode: networkaccess.ModePublicOnly, Found: false, Allowed: false}, nil
	case err != nil:
		return ResolutionResult{}, oops.E(oops.CodeUnexpected, err, "load mcp endpoint").LogError(ctx, logger)
	}

	project, err := projects_repo.New(db).GetProjectByID(ctx, endpoint.ProjectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return deniedResult(&endpoint, nil, nil, networkaccess.ModePrivateOnly), nil
	case err != nil:
		return ResolutionResult{}, oops.E(oops.CodeUnexpected, err, "load mcp endpoint project").LogError(ctx, logger)
	}
	if input.ExpectedOrganization != "" && project.OrganizationID != input.ExpectedOrganization {
		return deniedResult(&endpoint, nil, nil, networkaccess.ModePrivateOnly), nil
	}

	if endpoint.MetaMcpServerID.Valid {
		metaServer, err := metamcp_repo.New(db).GetMetaMCPServerByIDAndProjectID(ctx, metamcp_repo.GetMetaMCPServerByIDAndProjectIDParams{
			ID:        endpoint.MetaMcpServerID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return deniedResult(&endpoint, nil, nil, networkaccess.ModePrivateOnly), nil
		case err != nil:
			return ResolutionResult{}, oops.E(oops.CodeUnexpected, err, "load meta mcp server").LogError(ctx, logger)
		}
		mode, modeErr := networkaccess.Effective(metaServer.NetworkAccessMode)
		if modeErr != nil {
			return deniedResult(&endpoint, nil, &metaServer, networkaccess.ModePrivateOnly), nil
		}
		if metaServer.OrganizationID != project.OrganizationID || metaServer.Visibility == metamcp_visibility.Disabled || !mode.Allows(input.Surface) {
			return deniedResult(&endpoint, nil, &metaServer, mode), nil
		}
		return ResolutionResult{Endpoint: &endpoint, Server: nil, MetaServer: &metaServer, Mode: mode, Found: true, Allowed: true}, nil
	}

	server, err := mcpservers_repo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        endpoint.McpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return deniedResult(&endpoint, nil, nil, networkaccess.ModePrivateOnly), nil
	case err != nil:
		return ResolutionResult{}, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, logger)
	}
	mode, modeErr := networkaccess.Effective(server.NetworkAccessMode)
	if modeErr != nil {
		return deniedResult(&endpoint, &server, nil, networkaccess.ModePrivateOnly), nil
	}
	switch server.Visibility {
	case mcpservers.VisibilityPublic, mcpservers.VisibilityPrivate:
		// Known serving states continue to network-surface policy below.
	default:
		// Disabled and unrecognized values fail closed. An endpoint row owns
		// its address, so this is an authoritative denial rather than a miss.
		return deniedResult(&endpoint, &server, nil, mode), nil
	}
	if !mode.Allows(input.Surface) {
		return deniedResult(&endpoint, &server, nil, mode), nil
	}
	return ResolutionResult{Endpoint: &endpoint, Server: &server, MetaServer: nil, Mode: mode, Found: true, Allowed: true}, nil
}

func deniedResult(endpoint *repo.McpEndpoint, server *mcpservers_repo.McpServer, metaServer *metamcp_repo.MetaMcpServer, mode networkaccess.Mode) ResolutionResult {
	return ResolutionResult{Endpoint: endpoint, Server: server, MetaServer: metaServer, Mode: mode, Found: true, Allowed: false}
}

// ErrPolicyDenied marks a terminal address result whose public representation
// is 404. Callers that support legacy fallback must not fall through when this
// cause is present.
var ErrPolicyDenied = errors.New("mcp endpoint policy denied")

// IsPolicyDenied reports whether an error is the terminal policy outcome.
func IsPolicyDenied(err error) bool {
	return errors.Is(err, ErrPolicyDenied)
}

// IsAddressMiss reports whether an error is a genuine address miss that may
// fall back to legacy toolset lookup rather than an authoritative policy denial.
func IsAddressMiss(err error) bool {
	var shareErr *oops.ShareableError
	return errors.As(err, &shareErr) && shareErr.Code == oops.CodeNotFound && !IsPolicyDenied(err)
}

// BySlugAndCustomDomain preserves the public resolver signature while callers
// migrate to ResolutionResult. A genuine miss remains CodeNotFound. Policy
// denial is also represented as 404 to clients, but is authoritative and must
// never be converted into legacy fallback by callers using Resolve directly.
func BySlugAndCustomDomain(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, slug string) (*repo.McpEndpoint, *mcpservers_repo.McpServer, *metamcp_repo.MetaMcpServer, error) {
	input, err := FromContext(ctx, slug)
	if err != nil {
		return nil, nil, nil, oops.E(oops.CodeNotFound, errors.Join(ErrPolicyDenied, err), "mcp endpoint not found")
	}
	result, err := Resolve(ctx, db, logger, input)
	if err != nil {
		return nil, nil, nil, err
	}
	if !result.Found {
		return nil, nil, nil, oops.E(oops.CodeNotFound, nil, "mcp endpoint not found")
	}
	if !result.Allowed {
		return nil, nil, nil, oops.E(oops.CodeNotFound, ErrPolicyDenied, "mcp endpoint not found")
	}
	return result.Endpoint, result.Server, result.MetaServer, nil
}
