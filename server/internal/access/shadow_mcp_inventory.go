package access

import (
	"context"
	"errors"
	"math"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	shadowMCPInventoryMaxPageLimit      = 200
	shadowMCPInventoryUsageTraceLimit   = 50000
	shadowMCPInventoryPageLookaheadSize = 1
)

func (s *Service) ListShadowMCPInventory(ctx context.Context, payload *gen.ListShadowMCPInventoryPayload) (*gen.ListShadowMCPInventoryResult, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, s.logger)
	}
	if err := s.requireProjectInOrganization(ctx, ac.ActiveOrganizationID, projectID); err != nil {
		return nil, err
	}

	limit, err := shadowMCPInventoryLimit(payload.Limit)
	if err != nil {
		return nil, err
	}

	chRepo := telemetryrepo.New(s.chConn)
	inventoryRows, err := chRepo.ListShadowMCPInventoryURLs(ctx, telemetryrepo.ListShadowMCPInventoryURLsParams{
		GramProjectID: projectID.String(),
		Limit:         limit + shadowMCPInventoryPageLookaheadSize,
		Cursor:        pointerStringValue(payload.Cursor),
	})
	if err != nil {
		if errors.Is(err, telemetryrepo.ErrInvalidShadowMCPInventoryURLCursor) {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory urls").LogError(ctx, s.logger)
	}

	var nextCursor *string
	if len(inventoryRows) > limit {
		cursor, err := telemetryrepo.EncodeShadowMCPInventoryURLCursor(inventoryRows[limit-1])
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "encode shadow mcp inventory cursor").LogError(ctx, s.logger)
		}
		nextCursor = &cursor
		inventoryRows = inventoryRows[:limit]
	}

	usageByURL := map[string]telemetryrepo.ShadowMCPInventoryUsageRow{}
	if len(inventoryRows) > 0 {
		usageRows, err := chRepo.ListShadowMCPInventoryUsage(ctx, telemetryrepo.ListShadowMCPInventoryUsageParams{
			GramProjectID:       projectID.String(),
			CanonicalServerURLs: shadowMCPInventoryCanonicalURLs(inventoryRows),
			Limit:               shadowMCPInventoryUsageTraceLimit,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory usage").LogError(ctx, s.logger)
		}
		usageByURL = shadowMCPInventoryUsageByURL(usageRows)
	}

	servers := make([]*gen.ShadowMCPInventoryServer, 0, len(inventoryRows))
	for _, row := range inventoryRows {
		inventoryURL := shadowmcp.InventoryURL{
			CanonicalURL: row.CanonicalServerURL,
			URLHost:      row.URLHost,
		}
		accessState, err := s.resolveShadowMCPInventoryAccessState(ctx, ac.ActiveOrganizationID, projectID.String(), inventoryURL)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve shadow mcp inventory access state").LogError(ctx, s.logger)
		}
		servers = append(servers, buildShadowMCPInventoryServer(row, usageByURL[row.CanonicalServerURL], accessState))
	}

	return &gen.ListShadowMCPInventoryResult{
		Servers:    servers,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) ListShadowMCPInventoryUsers(ctx context.Context, payload *gen.ListShadowMCPInventoryUsersPayload) (*gen.ListShadowMCPInventoryUsersResult, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, s.logger)
	}
	if err := s.requireProjectInOrganization(ctx, ac.ActiveOrganizationID, projectID); err != nil {
		return nil, err
	}

	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(payload.ServerURL)
	if !ok {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid shadow mcp server url").LogError(ctx, s.logger)
	}

	limit, err := shadowMCPInventoryLimit(payload.Limit)
	if err != nil {
		return nil, err
	}

	chRepo := telemetryrepo.New(s.chConn)
	userRows, err := chRepo.ListShadowMCPInventoryUsers(ctx, telemetryrepo.ListShadowMCPInventoryUsersParams{
		GramProjectID:      projectID.String(),
		CanonicalServerURL: inventoryURL.CanonicalURL,
		Limit:              limit + shadowMCPInventoryPageLookaheadSize,
		Cursor:             pointerStringValue(payload.Cursor),
	})
	if err != nil {
		if errors.Is(err, telemetryrepo.ErrInvalidShadowMCPInventoryUserCursor) {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory users").LogError(ctx, s.logger)
	}

	var nextCursor *string
	if len(userRows) > limit {
		cursor, err := telemetryrepo.EncodeShadowMCPInventoryUserCursor(userRows[limit-1])
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "encode shadow mcp inventory user cursor").LogError(ctx, s.logger)
		}
		nextCursor = &cursor
		userRows = userRows[:limit]
	}

	users := make([]*gen.ShadowMCPInventoryUser, 0, len(userRows))
	for _, row := range userRows {
		users = append(users, buildShadowMCPInventoryUser(row))
	}

	return &gen.ListShadowMCPInventoryUsersResult{
		Users:      users,
		NextCursor: nextCursor,
	}, nil
}

func shadowMCPInventoryLimit(limit int) (int, error) {
	if limit < 1 {
		return 0, oops.E(oops.CodeBadRequest, nil, "limit must be greater than or equal to 1")
	}
	if limit > shadowMCPInventoryMaxPageLimit {
		return 0, oops.E(oops.CodeBadRequest, nil, "limit must be less than or equal to %d", shadowMCPInventoryMaxPageLimit)
	}
	return limit, nil
}

func shadowMCPInventoryUsageByURL(rows []telemetryrepo.ShadowMCPInventoryUsageRow) map[string]telemetryrepo.ShadowMCPInventoryUsageRow {
	out := make(map[string]telemetryrepo.ShadowMCPInventoryUsageRow, len(rows))
	for _, row := range rows {
		out[row.CanonicalServerURL] = row
	}
	return out
}

func buildShadowMCPInventoryServer(row telemetryrepo.ShadowMCPInventoryURLRow, usage telemetryrepo.ShadowMCPInventoryUsageRow, accessState shadowMCPInventoryAccessState) *gen.ShadowMCPInventoryServer {
	var serverName *string
	serverNameValue := row.ServerName
	if serverNameValue == "" {
		serverNameValue = usage.ServerName
	}
	if serverNameValue != "" {
		serverName = &serverNameValue
	}
	topUsers := usage.TopUsers
	if topUsers == nil {
		topUsers = []string{}
	}

	return &gen.ShadowMCPInventoryServer{
		CanonicalServerURL: row.CanonicalServerURL,
		URLHost:            row.URLHost,
		ServerName:         serverName,
		FirstSeen:          formatTimeValue(row.FirstSeen),
		LastSeen:           formatTimeValue(row.LastSeen),
		LastCalled:         formatTimePtrValue(usage.LastCalled),
		ObservedUseCount:   shadowMCPInventoryCount(usage.CallCount),
		UserCount:          shadowMCPInventoryCount(usage.UserCount),
		TopUsers:           topUsers,
		Access:             accessState.Access,
		Rule:               buildShadowMCPInventoryAccessRuleMatch(accessState.Rule),
	}
}

func buildShadowMCPInventoryUser(row telemetryrepo.ShadowMCPInventoryUserRow) *gen.ShadowMCPInventoryUser {
	return &gen.ShadowMCPInventoryUser{
		UserKey:          row.UserKey,
		LastCalled:       formatTimeValue(row.LastCalled),
		ObservedUseCount: shadowMCPInventoryCount(row.CallCount),
	}
}

func shadowMCPInventoryCanonicalURLs(rows []telemetryrepo.ShadowMCPInventoryURLRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.CanonicalServerURL != "" {
			out = append(out, row.CanonicalServerURL)
		}
	}
	return out
}

func buildShadowMCPInventoryAccessRuleMatch(match *shadowMCPInventoryAccessRuleMatch) *gen.ShadowMCPInventoryAccessRuleMatch {
	if match == nil {
		return nil
	}

	var projectID *string
	if match.ProjectID != "" {
		projectID = &match.ProjectID
	}
	return &gen.ShadowMCPInventoryAccessRuleMatch{
		ID:           match.ID,
		ProjectID:    projectID,
		AccessScope:  match.AccessScope,
		Disposition:  match.Disposition,
		MatchBreadth: match.MatchKind,
		MatchValue:   match.MatchValue,
		DisplayName:  match.DisplayName,
	}
}

func shadowMCPInventoryCount(value uint64) int {
	if value > uint64(math.MaxInt) {
		return math.MaxInt
	}
	return int(value) // #nosec G115 -- guarded by math.MaxInt check above.
}

func pointerStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
