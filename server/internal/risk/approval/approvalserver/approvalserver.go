// Package approvalserver gives a project its approvals MCP server: the server
// agents call request_approval on. Speakeasy creates it with the project's
// first approval policy and adds it to the Default plugin, so agents that use
// the plugin already have it. Agents with a key call it with the key; others
// sign the user in through its issuer. After that it is an ordinary plugin
// server.
package approvalserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	endpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/serverissuer"
)

const (
	// Name is the approvals server's display name.
	Name = "Speakeasy Approvals"

	// endpointSuffix ends the endpoint slug, after the organization and
	// project slugs that make it unique.
	endpointSuffix = "approvals"

	// maxEndpointSlugLen is the mcp_endpoints.slug column limit.
	maxEndpointSlugLen = 128
)

// Server is a project's approvals server.
type Server struct {
	// ID is the mcp_servers row.
	ID uuid.UUID

	// Slug is the server's project-unique slug.
	Slug string

	// EndpointSlug addresses it at /mcp/<slug>. Empty when an admin removed
	// its endpoint.
	EndpointSlug string
}

// Outcome is what Ensure did.
type Outcome struct {
	// Server is the project's approvals server.
	Server Server

	// Created is true when this call created the server.
	Created bool

	// AddedToDefaultPlugin is true when this call added the server to the
	// project's Default plugin, which then needs a republish.
	AddedToDefaultPlugin bool

	// DefaultPluginCreated is true when the Default plugin did not exist yet
	// and this call created it.
	DefaultPluginCreated bool
}

// Find returns the project's approvals server, or false when it has none.
func Find(ctx context.Context, db repo.DBTX, projectID uuid.UUID) (Server, bool, error) {
	row, err := repo.New(db).GetBuiltinMCPServer(ctx, repo.GetBuiltinMCPServerParams{ProjectID: projectID, BuiltinBackend: conv.ToPGText(approval.BuiltinBackend)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{ID: uuid.Nil, Slug: "", EndpointSlug: ""}, false, nil
	}
	if err != nil {
		return Server{ID: uuid.Nil, Slug: "", EndpointSlug: ""}, false, fmt.Errorf("find approvals server: %w", err)
	}
	return Server{ID: row.ID, Slug: row.Slug.String, EndpointSlug: row.EndpointSlug}, true, nil
}

// Ensure creates the project's approvals server the first time it is called
// for the project, with an endpoint, and adds it to the Default plugin. Later
// calls find the server and change nothing, so an admin who removes it from a
// plugin is not overruled. It runs in the caller's transaction; the caller
// commits, then triggers the plugin republish when AddedToDefaultPlugin.
func Ensure(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext) (Outcome, error) {
	none := Outcome{Server: Server{ID: uuid.Nil, Slug: "", EndpointSlug: ""}, Created: false, AddedToDefaultPlugin: false, DefaultPluginCreated: false}
	if authCtx == nil || authCtx.ProjectID == nil || authCtx.ProjectSlug == nil || authCtx.OrganizationSlug == "" {
		return none, errors.New("ensure approvals server: an organization and project are required")
	}
	projectID := *authCtx.ProjectID
	if existing, ok, err := Find(ctx, tx, projectID); err != nil || ok {
		return Outcome{Server: existing, Created: false, AddedToDefaultPlugin: false, DefaultPluginCreated: false}, err
	}

	serverID, err := uuid.NewV7()
	if err != nil {
		return none, fmt.Errorf("generate approvals server ID: %w", err)
	}
	// The same shape as every other server's slug: the name, then the ID's
	// last four characters.
	id := serverID.String()
	slug := conv.ToSlug(Name) + "-" + id[len(id)-4:]
	created, err := repo.New(tx).CreateBuiltinMCPServer(ctx, repo.CreateBuiltinMCPServerParams{
		ID:                serverID,
		ProjectID:         projectID,
		Name:              conv.ToPGText(Name),
		Slug:              conv.ToPGText(slug),
		BuiltinBackend:    conv.ToPGText(approval.BuiltinBackend),
		Visibility:        visibility.Private,
		NetworkAccessMode: networkaccess.Storage(networkaccess.ModePublicOnly),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Another transaction created it first.
		existing, _, findErr := Find(ctx, tx, projectID)
		return Outcome{Server: existing, Created: false, AddedToDefaultPlugin: false, DefaultPluginCreated: false}, findErr
	}
	if err != nil {
		return none, fmt.Errorf("create approvals server: %w", err)
	}
	// Callers without a key sign in through the server's own issuer, as on a
	// remote server. It is minted after the create, so a create that lost a
	// race to another transaction leaves no issuer behind.
	issuerID, err := serverissuer.Mint(ctx, tx, authCtx.ActiveOrganizationID, projectID, created.Slug.String)
	if err != nil {
		return none, fmt.Errorf("create approvals server issuer: %w", err)
	}
	if err := repo.New(tx).SetBuiltinMCPServerUserSessionIssuer(ctx, repo.SetBuiltinMCPServerUserSessionIssuerParams{
		UserSessionIssuerID: issuerID,
		ID:                  created.ID,
		ProjectID:           projectID,
	}); err != nil {
		return none, fmt.Errorf("attach approvals server issuer: %w", err)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	if err := auditLogger.LogMcpServerCreate(ctx, tx, audit.LogMcpServerCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            actor,
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(created.ID),
		McpServerName:    Name,
		McpServerSlug:    created.Slug.String,
	}); err != nil {
		return none, fmt.Errorf("audit approvals server create: %w", err)
	}

	endpointSlug, err := createEndpoint(ctx, tx, auditLogger, authCtx, created.ID)
	if err != nil {
		return none, err
	}

	attached, err := plugins.AttachToDefaultPluginAuditedWithOutcome(ctx, tx, auditLogger, authCtx, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,
		ToolsetID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    uuid.NullUUID{UUID: created.ID, Valid: true},
		DisplayName:    approval.MCPServerName,
	})
	if err != nil {
		return none, fmt.Errorf("add approvals server to the Default plugin: %w", err)
	}
	return Outcome{
		Server:               Server{ID: created.ID, Slug: created.Slug.String, EndpointSlug: endpointSlug},
		Created:              true,
		AddedToDefaultPlugin: attached.Attached,
		DefaultPluginCreated: attached.PluginCreated,
	}, nil
}

// createEndpoint gives the server a platform endpoint. Endpoint slugs start
// with the organization slug, and the project slug keeps each project's
// approvals server apart; a taken slug gets a longer and longer suffix of the
// server ID.
func createEndpoint(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, authCtx *contextvalues.AuthContext, serverID uuid.UUID) (string, error) {
	noDomain := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	for _, slug := range endpointSlugCandidates(authCtx.OrganizationSlug, *authCtx.ProjectSlug, serverID) {
		// The same lock and unified slug check the endpoints service uses, so
		// the slug cannot collide with an endpoint or a toolset's MCP slug.
		endpoints := endpointsrepo.New(tx)
		if err := endpoints.LockSlugScope(ctx, endpointsrepo.LockSlugScopeParams{CustomDomainID: noDomain, Slug: slug}); err != nil {
			return "", fmt.Errorf("lock approvals endpoint slug: %w", err)
		}
		available, err := endpoints.CheckUnifiedSlugAvailability(ctx, endpointsrepo.CheckUnifiedSlugAvailabilityParams{
			Slug:               slug,
			CustomDomainID:     noDomain,
			OrganizationID:     authCtx.ActiveOrganizationID,
			ExcludeToolsetID:   noDomain,
			ExcludeMcpServerID: uuid.NullUUID{UUID: serverID, Valid: true},
			SkipDomainCheck:    false,
		})
		if err != nil {
			return "", fmt.Errorf("check approvals endpoint slug: %w", err)
		}
		if !available.Bool {
			continue
		}
		endpoint, err := endpoints.CreateMCPEndpoint(ctx, endpointsrepo.CreateMCPEndpointParams{
			ProjectID:       *authCtx.ProjectID,
			CustomDomainID:  noDomain,
			McpServerID:     uuid.NullUUID{UUID: serverID, Valid: true},
			MetaMcpServerID: noDomain,
			Slug:            slug,
		})
		if err != nil {
			return "", fmt.Errorf("create approvals endpoint: %w", err)
		}
		if err := auditLogger.LogMcpEndpointCreate(ctx, tx, audit.LogMcpEndpointCreateEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			McpEndpointURN:   urn.NewMcpEndpoint(endpoint.ID),
			Slug:             endpoint.Slug,
		}); err != nil {
			return "", fmt.Errorf("audit approvals endpoint create: %w", err)
		}
		return endpoint.Slug, nil
	}
	return "", errors.New("no free endpoint slug for the approvals server")
}

// endpointSlugCandidates are the endpoint slugs to try in order: the
// organization, project and suffix, then the same with 4, 8 and 12 characters
// of the server ID's random tail. Each cuts the base so the slug fits the
// endpoint column. Slugs are ASCII, so a byte cut is a character cut.
func endpointSlugCandidates(organizationSlug, projectSlug string, serverID uuid.UUID) []string {
	base := fmt.Sprintf("%s-%s-%s", organizationSlug, projectSlug, endpointSuffix)
	id := serverID.String()
	suffixes := []string{"", "-" + id[len(id)-4:], "-" + id[len(id)-8:], "-" + id[len(id)-12:]}
	candidates := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		keep := min(len(base), maxEndpointSlugLen-len(suffix))
		candidates = append(candidates, strings.TrimRight(base[:keep], "-")+suffix)
	}
	return candidates
}
