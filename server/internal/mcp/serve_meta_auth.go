package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// derivedMemberAuthSupported admits only authentication the derived gateway
// can supply. It checks capability, not whether a person has connected yet:
// an issuer-backed OAuth member must remain visible so they can connect it.
func (s *Service) derivedMemberAuthSupported(ctx context.Context, member metaMember) (bool, error) {
	if member.backend != metaMemberBackendHosted {
		if !member.remoteServerID.Valid {
			return true, nil
		}
		headers, err := remotemcp.NewHeaders(s.logger, s.db, s.enc).ListHeaders(ctx, member.remoteServerID.UUID, false)
		if err != nil {
			return false, fmt.Errorf("load member headers: %w", err)
		}
		for _, header := range headers {
			configured := proxy.ConfiguredHeader{
				Name:                   header.Name,
				StaticValue:            header.Value.String,
				ValueFromRequestHeader: header.ValueFromRequestHeader.String,
				IsRequired:             header.IsRequired,
			}
			if !derivedMemberHeaderSupported(configured, member.remoteSessionIssuerID.Valid) {
				return false, nil
			}
		}
		return true, nil
	}

	toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        member.toolsetID.UUID,
		ProjectID: member.projectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("load member auth toolset: %w", err)
	}
	// Same rule as loadMemberToolset.
	if member.serverID == toolset.ID && !toolset.McpEnabled {
		return false, nil
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
			return false, fmt.Errorf("load member auth environment: %w", err)
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
	if member.remoteSessionIssuerID.Valid {
		// Stands in for the token a person gets by connecting the member.
		inputs.oauthTokenInputs = []oauthTokenInputs{{ //nolint:gosec // capability probe marker, not a credential
			remoteSessionIssuerID: member.remoteSessionIssuerID,
			securityKeys:          nil,
			Token:                 "gateway-oauth-capability",
		}}
	}
	return s.checkToolsetSecurity(ctx, &toolset, toolset.McpIsPublic, &inputs)
}

func derivedMemberHeaderSupported(header proxy.ConfiguredHeader, oauth bool) bool {
	if !header.IsRequired {
		return true
	}
	// The proxy's upstream OAuth token overrides configured Authorization.
	if oauth && strings.EqualFold(header.Name, "Authorization") {
		return true
	}
	if err := proxy.CheckRemoteHeader(header); err != nil {
		return false
	}
	// A gateway's synthesized request carries no caller headers.
	var req http.Request
	req.Header = make(http.Header)
	value, err := header.Resolve(&req)
	return err == nil && value != "" && proxy.ValidateHeaderValue(value) == nil
}
