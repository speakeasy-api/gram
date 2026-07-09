package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

const (
	shadowMCPInventoryMaxPageLimit      = 200
	shadowMCPInventoryUsageTraceLimit   = 50000
	shadowMCPInventoryPageLookaheadSize = 1

	shadowMCPInventoryAccessNone    = "none"
	shadowMCPInventoryAccessAllowed = "allowed"
	shadowMCPInventoryAccessBlocked = "blocked"

	shadowMCPInventoryBypassStatusRequested = "requested"
	shadowMCPInventoryBypassTargetKind      = "shadow_mcp_server"
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

	policyState, err := s.shadowMCPInventoryPolicyState(ctx, ac.ActiveOrganizationID, projectID, shadowMCPInventoryCanonicalURLs(inventoryRows))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load shadow mcp inventory policy state").LogError(ctx, s.logger)
	}

	servers := make([]*gen.ShadowMCPInventoryServer, 0, len(inventoryRows))
	for _, row := range inventoryRows {
		servers = append(servers, buildShadowMCPInventoryServer(row, usageByURL[row.CanonicalServerURL], policyState.forURL(row.CanonicalServerURL)))
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

type shadowMCPInventoryPolicyState struct {
	hasBlockingPolicy bool
	allowedPolicyIDs  map[string][]string
	requestsByURL     map[string]shadowMCPInventoryRequestState
}

type shadowMCPInventoryRowState struct {
	Access           string
	RequestCount     int
	LatestRequest    *gen.ShadowMCPInventoryRequestSummary
	AllowedPolicyIDs []string
}

type shadowMCPInventoryRequestState struct {
	Count  int
	Latest *gen.ShadowMCPInventoryRequestSummary
	At     time.Time
}

func (s *Service) shadowMCPInventoryPolicyState(ctx context.Context, organizationID string, projectID uuid.UUID, canonicalURLs []string) (shadowMCPInventoryPolicyState, error) {
	state := shadowMCPInventoryPolicyState{
		hasBlockingPolicy: false,
		allowedPolicyIDs:  map[string][]string{},
		requestsByURL:     map[string]shadowMCPInventoryRequestState{},
	}
	if len(canonicalURLs) == 0 {
		return state, nil
	}

	canonicalURLSet := make(map[string]struct{}, len(canonicalURLs))
	for _, canonicalURL := range canonicalURLs {
		if canonicalURL != "" {
			canonicalURLSet[canonicalURL] = struct{}{}
		}
	}
	if len(canonicalURLSet) == 0 {
		return state, nil
	}

	repo := riskrepo.New(s.db)
	policies, err := repo.ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return state, fmt.Errorf("listing enabled shadow mcp policies: %w", err)
	}

	blockingPolicyIDs := make(map[string]struct{}, len(policies))
	for _, policy := range policies {
		if policy.Action != "block" {
			continue
		}
		state.hasBlockingPolicy = true
		policyID := policy.ID.String()
		blockingPolicyIDs[policyID] = struct{}{}

		grants, err := authz.ListGrantsForResource(ctx, s.db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		})
		if err != nil {
			return state, fmt.Errorf("listing grants for shadow mcp policy: %w", err)
		}
		for _, grant := range grants {
			if grant.Effect != authz.PolicyEffectAllow {
				continue
			}
			serverURL := grant.Selector[authz.SelectorKeyServerURL]
			if _, ok := canonicalURLSet[serverURL]; !ok {
				continue
			}
			state.allowedPolicyIDs[serverURL] = append(state.allowedPolicyIDs[serverURL], policyID)
		}
	}
	if len(blockingPolicyIDs) == 0 {
		return state, nil
	}

	requests, err := repo.ListRiskPolicyBypassRequests(ctx, riskrepo.ListRiskPolicyBypassRequestsParams{
		ProjectID:    projectID,
		RiskPolicyID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Status:       conv.ToPGText(shadowMCPInventoryBypassStatusRequested),
	})
	if err != nil {
		return state, fmt.Errorf("listing shadow mcp bypass requests: %w", err)
	}
	for _, request := range requests {
		if _, ok := blockingPolicyIDs[request.RiskPolicyID.String()]; !ok {
			continue
		}
		if conv.FromPGTextOrEmpty[string](request.TargetKind) != shadowMCPInventoryBypassTargetKind {
			continue
		}
		dimensions, err := shadowMCPInventoryBypassDimensions(request.TargetDimensions)
		if err != nil {
			return state, err
		}
		serverURL := dimensions[authz.SelectorKeyServerURL]
		if _, ok := canonicalURLSet[serverURL]; !ok {
			continue
		}
		updatedAt := request.UpdatedAt.Time
		summary := &gen.ShadowMCPInventoryRequestSummary{
			ID:              request.ID.String(),
			PolicyID:        request.RiskPolicyID.String(),
			RequesterUserID: request.RequesterUserID,
			RequesterEmail:  conv.FromPGTextOrEmpty[string](request.RequesterEmail),
			RequestedAt:     conv.FromPGTimestamptz(request.CreatedAt),
		}
		current := state.requestsByURL[serverURL]
		current.Count++
		if current.Latest == nil || updatedAt.After(current.At) {
			current.Latest = summary
			current.At = updatedAt
		}
		state.requestsByURL[serverURL] = current
	}

	for serverURL, policyIDs := range state.allowedPolicyIDs {
		slices.Sort(policyIDs)
		state.allowedPolicyIDs[serverURL] = slices.Compact(policyIDs)
	}

	return state, nil
}

func (s shadowMCPInventoryPolicyState) forURL(canonicalURL string) shadowMCPInventoryRowState {
	requestState := s.requestsByURL[canonicalURL]
	allowedPolicyIDs := s.allowedPolicyIDs[canonicalURL]
	access := shadowMCPInventoryAccessNone
	switch {
	case len(allowedPolicyIDs) > 0:
		access = shadowMCPInventoryAccessAllowed
	case s.hasBlockingPolicy:
		access = shadowMCPInventoryAccessBlocked
	}

	return shadowMCPInventoryRowState{
		Access:           access,
		RequestCount:     requestState.Count,
		LatestRequest:    requestState.Latest,
		AllowedPolicyIDs: allowedPolicyIDs,
	}
}

func shadowMCPInventoryBypassDimensions(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	dimensions := map[string]string{}
	if err := json.Unmarshal(raw, &dimensions); err != nil {
		return nil, fmt.Errorf("unmarshaling shadow mcp inventory bypass dimensions: %w", err)
	}
	return dimensions, nil
}

func buildShadowMCPInventoryServer(row telemetryrepo.ShadowMCPInventoryURLRow, usage telemetryrepo.ShadowMCPInventoryUsageRow, rowState shadowMCPInventoryRowState) *gen.ShadowMCPInventoryServer {
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
		Access:             rowState.Access,
		RequestCount:       rowState.RequestCount,
		LatestRequest:      rowState.LatestRequest,
		AllowedPolicyIds:   rowState.AllowedPolicyIDs,
	}
}

func buildShadowMCPInventoryUser(row telemetryrepo.ShadowMCPInventoryUserRow) *gen.ShadowMCPInventoryUser {
	return &gen.ShadowMCPInventoryUser{
		UserKey:          row.UserKey,
		Name:             nil,
		Email:            conv.PtrEmpty(row.UserEmail),
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
