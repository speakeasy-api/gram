package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// derivedMemberAuth reports whether a derived gateway can authenticate the
// member, and whether that takes the upstream OAuth token a person gets by
// connecting it through the gateway, by memberNeedsSignIn. It checks
// capability, not whether a person has connected yet: an OAuth member stays
// supported so they can connect it.
func (s *Service) derivedMemberAuth(ctx context.Context, shadow *mandatoryShadow, shadowSurface string, member metaMember) (supported bool, needsOAuth bool, err error) {
	oauth := memberNeedsSignIn(member)
	if member.backend != metaMemberBackendHosted {
		if !member.remoteServerID.Valid {
			return true, oauth, nil
		}
		rows, err := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, member.remoteServerID.UUID, false)
		if err != nil {
			return false, false, fmt.Errorf("load member headers: %w", err)
		}
		headers := make([]proxy.ConfiguredHeader, 0, len(rows))
		for _, row := range rows {
			headers = append(headers, proxy.ConfiguredHeader{
				Name:                   row.Name,
				StaticValue:            row.Value.String,
				ValueFromRequestHeader: row.ValueFromRequestHeader.String,
				IsRequired:             row.IsRequired,
			})
		}
		supported, needsOAuth := remoteMemberAuth(headers, oauth)
		return supported, needsOAuth, nil
	}

	toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        member.toolsetID.UUID,
		ProjectID: member.projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, false, nil
	case err != nil:
		return false, false, fmt.Errorf("load member auth toolset: %w", err)
	}
	toolset, _, err = hostedmcp.LoadEffective(ctx, s.db, toolset)
	if err != nil {
		return false, false, fmt.Errorf("load member auth hosting state: %w", err)
	}
	// Same rules as loadMemberToolset. Derived gateways always have a
	// signed-in caller.
	if member.serverID == toolset.ID && !toolset.McpEnabled {
		return false, false, nil
	}
	allowed, err := s.memberToolsetConnectAllowed(ctx, shadow, shadowSurface, true, &toolset)
	if err != nil || !allowed {
		return false, false, err
	}

	environment := toolset.DefaultEnvironmentSlug.String
	if member.environmentID.Valid {
		row, err := environmentsrepo.New(s.db).GetEnvironmentByID(ctx, environmentsrepo.GetEnvironmentByIDParams{
			ID:        member.environmentID.UUID,
			ProjectID: member.projectID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// Dangling environment: keep the toolset default.
		case err != nil:
			return false, false, fmt.Errorf("load member auth environment: %w", err)
		default:
			environment = row.Slug
		}
	}

	// Only the fields checkToolsetSecurity reads; these inputs never reach
	// tool dispatch.
	var inputs mcpInputs
	inputs.projectID = member.projectID
	inputs.toolset = toolset.Slug
	inputs.environment = environment
	inputs.authenticated = true
	satisfied, err := s.checkToolsetSecurity(ctx, &toolset, toolset.McpIsPublic, &inputs)
	if err != nil || satisfied || !oauth {
		return satisfied, false, err
	}
	// Stands in for the token a person gets by connecting the member.
	inputs.oauthTokenInputs = []oauthTokenInputs{{ //nolint:gosec // capability probe marker, not a credential
		remoteSessionIssuerID: member.remoteSessionIssuerID,
		securityKeys:          nil,
		Token:                 "gateway-oauth-capability",
	}}
	satisfied, err = s.checkToolsetSecurity(ctx, &toolset, toolset.McpIsPublic, &inputs)
	return satisfied, satisfied, err
}

// remoteMemberAuth applies derivedMemberAuth to a remote member's configured
// headers. A gateway's synthesized request carries no caller headers, so a
// required header must resolve on its own, unless it is Authorization on an
// OAuth member, whose upstream token replaces it. A member with an upstream
// sign-in and no static credential is assumed to need that sign-in.
func remoteMemberAuth(headers []proxy.ConfiguredHeader, oauth bool) (supported bool, needsOAuth bool) {
	static := false
	for _, header := range headers {
		switch {
		case !header.IsRequired:
		case headerResolvesWithoutCaller(header):
			static = true
		case oauth && strings.EqualFold(header.Name, "Authorization"):
			needsOAuth = true
		default:
			return false, false
		}
	}
	return true, oauth && (needsOAuth || !static)
}

func headerResolvesWithoutCaller(header proxy.ConfiguredHeader) bool {
	if err := proxy.CheckRemoteHeader(header); err != nil {
		return false
	}
	var req http.Request
	req.Header = make(http.Header)
	value, err := header.Resolve(&req)
	return err == nil && value != "" && proxy.ValidateHeaderValue(value) == nil
}
