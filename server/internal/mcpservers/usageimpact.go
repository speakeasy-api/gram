package mcpservers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/endpointaddr"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// usageImpactSubjectLimit bounds the subjects a usage impact lists. A confirm
// dialog only needs enough to show who is affected; the counts stay exact.
const usageImpactSubjectLimit = 50

// GetMcpServerUsageImpact previews who a breaking change to a server affects.
// It authorizes the caller as the matching update does, since the preview
// exists only to confirm one.
func (s *Service) GetMcpServerUsageImpact(ctx context.Context, payload *gen.GetMcpServerUsageImpactPayload) (*types.McpServerUsageImpact, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	projectID := *authCtx.ProjectID
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogWarn(ctx, logger)
	}
	server, err := s.requireServerWriteUnlocked(ctx, serverID, projectID, logger)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogMcpServerID(server.ID.String()))

	impact := &types.McpServerUsageImpact{
		ServerName:            ServerDisplayName(server),
		SignInRequired:        server.UserSessionIssuerID.Valid,
		SharedSignIn:          false,
		ActiveConnectionCount: 0,
		ActiveSubjectCount:    0,
		SharedConnectionCount: 0,
		LastUsedAt:            nil,
		Subjects:              []*types.McpServerUsageSubject{},
		SubjectsTruncated:     false,
	}
	if !server.UserSessionIssuerID.Valid {
		return impact, nil
	}
	issuerID := server.UserSessionIssuerID.UUID
	organizationID := authCtx.ActiveOrganizationID

	resources, platformPaths, err := s.serverSessionResources(ctx, organizationID, server)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve mcp server addresses").LogError(ctx, logger)
	}

	q := repo.New(s.db)
	servingCount, err := q.CountUserSessionIssuerServingServers(ctx, repo.CountUserSessionIssuerServingServersParams{
		OrganizationID:      organizationID,
		UserSessionIssuerID: issuerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count servers sharing the issuer").LogError(ctx, logger)
	}
	shared := servingCount > 1
	impact.SharedSignIn = shared

	summary, err := q.SummarizeMCPServerUsage(ctx, repo.SummarizeMCPServerUsageParams{
		OrganizationID:      organizationID,
		UserSessionIssuerID: issuerID,
		Resources:           resources,
		PlatformPaths:       platformPaths,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "summarize mcp server usage").LogError(ctx, logger)
	}
	impact.ActiveConnectionCount = int(summary.ConnectionCount)
	impact.ActiveSubjectCount = int(summary.SubjectCount)
	if shared {
		impact.SharedConnectionCount = int(summary.IssuerScopedCount)
	}
	impact.LastUsedAt = timestampPtr(summary.LastUsedAt)
	if summary.ConnectionCount == 0 {
		return impact, nil
	}

	rows, err := q.ListMCPServerUsageSubjects(ctx, repo.ListMCPServerUsageSubjectsParams{
		OrganizationID:      organizationID,
		RowLimit:            usageImpactSubjectLimit,
		UserSessionIssuerID: issuerID,
		Resources:           resources,
		PlatformPaths:       platformPaths,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp server usage subjects").LogError(ctx, logger)
	}
	for _, row := range rows {
		impact.Subjects = append(impact.Subjects, usageSubjectView(row, shared))
	}
	impact.SubjectsTruncated = impact.ActiveSubjectCount > len(impact.Subjects)
	return impact, nil
}

// serverSessionResources lists the RFC 8707 resources a session bound to this
// server can carry. Custom-domain addresses are full URLs. Platform addresses
// are paths, because the platform answers under more than one host and a
// platform slug names one server whichever host minted the session.
func (s *Service) serverSessionResources(ctx context.Context, organizationID string, server repo.McpServer) ([]string, []string, error) {
	rows, err := mcpendpointsrepo.New(s.db).ListAddressableMCPEndpointsByMCPServerIDs(ctx, mcpendpointsrepo.ListAddressableMCPEndpointsByMCPServerIDsParams{
		OrganizationID: organizationID,
		ProjectIds:     []uuid.UUID{server.ProjectID},
		McpServerIds:   []uuid.UUID{server.ID},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list mcp server endpoints: %w", err)
	}
	resources := []string{}
	platformPaths := []string{}
	for _, row := range rows {
		endpoint := row.McpEndpoint
		if !endpoint.CustomDomainID.Valid {
			platformPaths = append(platformPaths, platformPath(endpoint.Slug))
			continue
		}
		if !row.CustomDomain.Valid {
			// The domain is not serving, so nothing can be bound to it.
			continue
		}
		u, err := endpointaddr.EndpointURL(&endpoint, row.CustomDomain.String, "")
		if err != nil {
			return nil, nil, fmt.Errorf("render custom domain endpoint url: %w", err)
		}
		resources = append(resources, u)
	}
	legacy, err := legacyalias.PlatformServedSlugs(ctx, s.db, organizationID, []uuid.UUID{server.ProjectID}, []uuid.UUID{server.ID})
	if err != nil {
		return nil, nil, fmt.Errorf("list legacy platform slugs: %w", err)
	}
	if slug, ok := legacy[server.ID]; ok {
		platformPaths = append(platformPaths, platformPath(slug))
	}
	return resources, platformPaths, nil
}

func platformPath(slug string) string {
	return "/mcp/" + url.PathEscape(slug)
}

func usageSubjectView(row repo.ListMCPServerUsageSubjectsRow, sharedSignIn bool) *types.McpServerUsageSubject {
	subjectType, _, _ := strings.Cut(row.SubjectUrn, ":")
	var displayName *string
	switch {
	case row.UserDisplayName.Valid && row.UserDisplayName.String != "":
		displayName = conv.FromPGText[string](row.UserDisplayName)
	case row.ApiKeyName.Valid:
		displayName = conv.FromPGText[string](row.ApiKeyName)
	case row.AgentName.Valid:
		displayName = conv.FromPGText[string](row.AgentName)
	}
	clientNames := row.ClientNames
	if clientNames == nil {
		clientNames = []string{}
	}
	return &types.McpServerUsageSubject{
		SubjectUrn:      row.SubjectUrn,
		SubjectType:     subjectType,
		DisplayName:     displayName,
		Email:           conv.FromPGText[string](row.UserEmail),
		PhotoURL:        conv.FromPGText[string](row.UserPhotoUrl),
		ConnectionCount: int(row.ConnectionCount),
		LastUsedAt:      timestampPtr(row.LastUsedAt),
		ClientNames:     clientNames,
		// Only sessions issued for the whole sign-in may belong to another
		// server; one bound to this server's address certainly does not.
		SharedSignIn: sharedSignIn && row.IssuerScopedCount == row.ConnectionCount,
	}
}

func timestampPtr(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	formatted := t.Time.UTC().Format(time.RFC3339)
	return &formatted
}
