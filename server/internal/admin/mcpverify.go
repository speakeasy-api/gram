package admin

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// mcpVerificationProject is the Speakeasy-owned project staff add MCP servers
// to when they verify one with a real sign-in.
type mcpVerificationProject struct {
	organizationID string
	projectID      uuid.UUID
}

// SetMCPVerificationProject names the Speakeasy-owned project that MCP
// verification links open in. Unset leaves the links unavailable.
func (s *Service) SetMCPVerificationProject(organizationID string, projectID uuid.UUID) {
	if organizationID == "" || projectID == uuid.Nil {
		s.mcpVerification = nil
		return
	}
	s.mcpVerification = &mcpVerificationProject{organizationID: organizationID, projectID: projectID}
}

// GetMcpVerificationLink builds the customer dashboard link staff open to
// verify a remote MCP server by signing in and running its tools. It only
// reads: when Speakeasy's project already has a server for the URL the link
// opens its Playground, and otherwise the add-server page with the URL filled
// in, where staff confirm the addition themselves.
func (s *Service) GetMcpVerificationLink(ctx context.Context, p *gen.GetMcpVerificationLinkPayload) (*gen.AdminMcpVerificationLink, error) {
	if s.mcpVerification == nil || s.dashboardURL == nil || s.dashboardURL.Scheme == "" || s.dashboardURL.Host == "" {
		return nil, oops.E(oops.CodeUnavailable, nil, "MCP verification is not configured")
	}

	target := strings.TrimSpace(p.URL)
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, oops.E(oops.CodeBadRequest, err, "url must be an absolute HTTP or HTTPS address")
	}

	queries := repo.New(s.db)
	project, err := queries.AdminGetMcpVerificationProject(ctx, repo.AdminGetMcpVerificationProjectParams{
		ProjectID:      s.mcpVerification.projectID,
		OrganizationID: s.mcpVerification.organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnavailable, err, "Speakeasy's MCP verification project was not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load MCP verification project").LogError(ctx, s.logger)
	}

	projectPath := s.dashboardURL.JoinPath(project.OrganizationSlug, "projects", project.ProjectSlug)

	serverID, err := queries.AdminFindMcpServerByRemoteURL(ctx, repo.AdminFindMcpServerByRemoteURLParams{
		ProjectID: s.mcpVerification.projectID,
		Url:       target,
	})
	switch {
	case err == nil:
		link := projectPath.JoinPath("playground")
		link.RawQuery = url.Values{"mcpServer": {serverID.String()}}.Encode()
		return &gen.AdminMcpVerificationLink{URL: link.String(), Existing: true, ProjectName: project.ProjectName}, nil
	case errors.Is(err, pgx.ErrNoRows):
		query := url.Values{"url": {target}}
		if p.Name != nil && strings.TrimSpace(*p.Name) != "" {
			query.Set("name", strings.TrimSpace(*p.Name))
		}
		link := projectPath.JoinPath("mcp", "add", "remote")
		link.RawQuery = query.Encode()
		return &gen.AdminMcpVerificationLink{URL: link.String(), Existing: false, ProjectName: project.ProjectName}, nil
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "find MCP server for verification").LogError(ctx, s.logger)
	}
}
