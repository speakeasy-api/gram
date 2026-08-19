package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpapprovalrepo "github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	shadowMCPInventoryMaxPageLimit      = 200
	shadowMCPInventoryUsageTraceLimit   = 50000
	shadowMCPInventoryPageLookaheadSize = 1

	shadowMCPInventoryAccessNone    = "none"
	shadowMCPInventoryAccessAllowed = "allowed"
	shadowMCPInventoryAccessBlocked = "blocked"

	shadowMCPInventoryBypassStatusRequested = "requested"
	shadowMCPInventoryBypassStatusApproved  = "approved"
	shadowMCPInventoryBypassStatusDenied    = "denied"
	shadowMCPInventoryBypassStatusRevoked   = "revoked"
	shadowMCPInventoryBypassTargetKind      = "shadow_mcp_server"

	shadowMCPInventoryDecisionAllow = "allow"
	shadowMCPInventoryDecisionDeny  = "deny"

	shadowMCPTargetKindServerURL    = "server_url"
	shadowMCPTargetKindStdioCommand = "stdio_command"
)

func (s *Service) requireOrgAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	return ac, nil
}

func (s *Service) requireProjectInOrganization(ctx context.Context, organizationID string, projectID uuid.UUID) error {
	_, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             projectID,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "project not found").LogError(ctx, s.logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get project").LogError(ctx, s.logger)
	default:
		return nil
	}
}

func formatTimePtrValue(ts *time.Time) *string {
	if ts == nil || ts.IsZero() {
		return nil
	}
	value := ts.UTC().Format(time.RFC3339)
	return &value
}

func formatTimeValue(ts time.Time) string {
	if ts.IsZero() {
		return time.Time{}.UTC().Format(time.RFC3339)
	}
	return ts.UTC().Format(time.RFC3339)
}

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

	// The table is the union of what telemetry observed and what reviews
	// know: targets someone asked about (or an admin opened a dossier on)
	// that traffic has never shown appear once, on the first page, ahead of
	// the cursor-paginated observed set. Later pages are purely observed.
	var requestOnly []mcpapprovalrepo.ListApprovalRequestTargetsRow
	if payload.Cursor == nil {
		requestOnly, err = s.shadowMCPRequestOnlyTargets(ctx, chRepo, projectID)
		if err != nil {
			return nil, err
		}
	}

	policyURLs := shadowMCPInventoryCanonicalURLs(inventoryRows)
	for _, request := range requestOnly {
		if request.TargetKind == shadowMCPTargetKindServerURL {
			policyURLs = append(policyURLs, request.TargetKey)
		}
	}

	policyState, err := s.shadowMCPInventoryPolicyState(ctx, ac.ActiveOrganizationID, projectID, policyURLs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load shadow mcp inventory policy state").LogError(ctx, s.logger)
	}

	servers := make([]*gen.ShadowMCPInventoryServer, 0, len(requestOnly)+len(inventoryRows))
	for _, request := range requestOnly {
		servers = append(servers, buildShadowMCPRequestOnlyServer(request, policyState))
	}
	for _, row := range inventoryRows {
		servers = append(servers, buildShadowMCPInventoryServer(row, usageByURL[row.CanonicalServerURL], policyState.forURL(row.CanonicalServerURL), shadowMCPTargetKindServerURL))
	}

	return &gen.ListShadowMCPInventoryResult{
		Servers:    servers,
		NextCursor: nextCursor,
	}, nil
}

// shadowMCPRequestOnlyTargets lists the reviews whose targets telemetry has
// never observed: every stdio command, and the server URLs absent from the
// inventory. These are the rows only the review system knows about.
func (s *Service) shadowMCPRequestOnlyTargets(ctx context.Context, chRepo *telemetryrepo.Queries, projectID uuid.UUID) ([]mcpapprovalrepo.ListApprovalRequestTargetsRow, error) {
	requests, err := mcpapprovalrepo.New(s.db).ListApprovalRequestTargets(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list approval request targets").LogError(ctx, s.logger)
	}
	if len(requests) == 0 {
		return nil, nil
	}

	urlKeys := make([]string, 0, len(requests))
	for _, request := range requests {
		if request.TargetKind == shadowMCPTargetKindServerURL {
			urlKeys = append(urlKeys, request.TargetKey)
		}
	}

	observed := map[string]struct{}{}
	if len(urlKeys) > 0 {
		observedKeys, err := chRepo.ListExistingShadowMCPInventoryURLs(ctx, telemetryrepo.ListExistingShadowMCPInventoryURLsParams{
			GramProjectID:       projectID.String(),
			CanonicalServerURLs: urlKeys,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check observed shadow mcp urls").LogError(ctx, s.logger)
		}
		for _, key := range observedKeys {
			observed[key] = struct{}{}
		}
	}

	out := make([]mcpapprovalrepo.ListApprovalRequestTargetsRow, 0, len(requests))
	for _, request := range requests {
		if request.TargetKind == shadowMCPTargetKindServerURL {
			if _, ok := observed[request.TargetKey]; ok {
				continue
			}
		}
		out = append(out, request)
	}
	return out, nil
}

// buildShadowMCPRequestOnlyServer synthesizes a servers-table row from a
// review with no telemetry behind it: zero usage, zero seen-times (the
// never-observed sentinel), and the review carried as the row's approval
// state. Stdio commands have no URL host and no server page.
func buildShadowMCPRequestOnlyServer(request mcpapprovalrepo.ListApprovalRequestTargetsRow, policyState shadowMCPInventoryPolicyState) *gen.ShadowMCPInventoryServer {
	targetKind := shadowMCPTargetKindStdioCommand
	urlHost := ""
	rowState := shadowMCPInventoryRowState{
		Access:           shadowMCPInventoryAccessNone,
		RequestCount:     0,
		LatestRequest:    nil,
		ApprovalRequest:  nil,
		AllowedPolicyIDs: nil,
		BlockedPolicyIDs: nil,
	}
	if request.TargetKind == shadowMCPTargetKindServerURL {
		targetKind = shadowMCPTargetKindServerURL
		inventoryURL, _ := shadowmcp.CanonicalizeInventoryURL(request.TargetKey)
		urlHost = inventoryURL.URLHost
		rowState = policyState.forURL(request.TargetKey)
	}
	// The review is authoritative for its own row whether or not the batched
	// join saw it (the join only covers server_url targets).
	rowState.ApprovalRequest = &gen.ShadowMCPInventoryApprovalRequest{
		ID:                request.ID.String(),
		Status:            request.Status,
		RequesterCount:    int(request.RequesterCount),
		EvidenceChangedAt: conv.PtrEmpty(conv.FromPGTimestamptz(request.EvidenceChangedAt)),
	}

	row := telemetryrepo.ShadowMCPInventoryURLRow{
		CanonicalServerURL: request.TargetKey,
		URLHost:            urlHost,
		ServerName:         "",
		ServerNameOverride: "",
		FirstSeen:          time.Time{},
		LastSeen:           time.Time{},
		LastCalledUnixNano: 0,
		UpdatedAt:          request.UpdatedAt.Time,
	}
	usage := telemetryrepo.ShadowMCPInventoryUsageRow{
		CanonicalServerURL: request.TargetKey,
		ServerName:         "",
		FirstCalled:        nil,
		LastCalled:         nil,
		CallCount:          0,
		UserCount:          0,
		TopUsers:           []string{},
	}
	return buildShadowMCPInventoryServer(row, usage, rowState, targetKind)
}

func (s *Service) GetShadowMCPInventoryServer(ctx context.Context, payload *gen.GetShadowMCPInventoryServerPayload) (*gen.ShadowMCPInventoryServer, error) {
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

	chRepo := telemetryrepo.New(s.chConn)
	inventoryRow, err := shadowMCPInventoryURLForSlug(ctx, chRepo, projectID.String(), payload.ServerSlug)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get shadow mcp inventory url by slug").LogError(ctx, s.logger)
	}
	if inventoryRow == nil {
		// A server can be known only through its approval request — asked
		// for, never observed in traffic — and its page must still resolve.
		return s.shadowMCPServerFromApprovalRequest(ctx, ac.ActiveOrganizationID, projectID, payload.ServerSlug)
	}

	usageRows, err := chRepo.ListShadowMCPInventoryUsage(ctx, telemetryrepo.ListShadowMCPInventoryUsageParams{
		GramProjectID:       projectID.String(),
		CanonicalServerURLs: []string{inventoryRow.CanonicalServerURL},
		Limit:               shadowMCPInventoryUsageTraceLimit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory usage").LogError(ctx, s.logger)
	}
	usageByURL := shadowMCPInventoryUsageByURL(usageRows)

	policyState, err := s.shadowMCPInventoryPolicyState(ctx, ac.ActiveOrganizationID, projectID, []string{inventoryRow.CanonicalServerURL})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load shadow mcp inventory policy state").LogError(ctx, s.logger)
	}

	return buildShadowMCPInventoryServer(*inventoryRow, usageByURL[inventoryRow.CanonicalServerURL], policyState.forURL(inventoryRow.CanonicalServerURL), shadowMCPTargetKindServerURL), nil
}

// shadowMCPServerFromApprovalRequest resolves a server page slug against the
// project's approval requests when telemetry has never seen the server. The
// synthesized view carries the request's identity and timeline with zeroed
// usage, so a requested-but-unobserved server still has the one page where
// its review lives.
func (s *Service) shadowMCPServerFromApprovalRequest(ctx context.Context, organizationID string, projectID uuid.UUID, serverSlug string) (*gen.ShadowMCPInventoryServer, error) {
	requests, err := mcpapprovalrepo.New(s.db).ListServerURLApprovalRequests(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list server url approval requests").LogError(ctx, s.logger)
	}

	for _, request := range requests {
		if shadowmcp.ServerSlug(request.TargetKey) != serverSlug {
			continue
		}

		policyState, err := s.shadowMCPInventoryPolicyState(ctx, organizationID, projectID, []string{request.TargetKey})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load shadow mcp inventory policy state").LogError(ctx, s.logger)
		}

		inventoryURL, _ := shadowmcp.CanonicalizeInventoryURL(request.TargetKey)
		row := telemetryrepo.ShadowMCPInventoryURLRow{
			CanonicalServerURL: request.TargetKey,
			URLHost:            inventoryURL.URLHost,
			ServerName:         "",
			ServerNameOverride: "",
			// This branch is reached only when telemetry has never observed
			// the server, so it has no first/last seen: the zero times render
			// as the established never-observed sentinel rather than passing
			// the request's own timeline off as observations. The request
			// timeline lives on the approval request itself.
			FirstSeen:          time.Time{},
			LastSeen:           time.Time{},
			LastCalledUnixNano: 0,
			UpdatedAt:          request.UpdatedAt.Time,
		}
		usage := telemetryrepo.ShadowMCPInventoryUsageRow{
			CanonicalServerURL: request.TargetKey,
			ServerName:         "",
			FirstCalled:        nil,
			LastCalled:         nil,
			CallCount:          0,
			UserCount:          0,
			TopUsers:           []string{},
		}
		return buildShadowMCPInventoryServer(row, usage, policyState.forURL(request.TargetKey), shadowMCPTargetKindServerURL), nil
	}

	return nil, oops.E(oops.CodeNotFound, nil, "shadow mcp inventory url not found").LogError(ctx, s.logger)
}

func (s *Service) UpdateShadowMCPInventoryServerName(ctx context.Context, payload *gen.UpdateShadowMCPInventoryServerNamePayload) error {
	_, projectID, inventoryURL, err := s.shadowMCPInventoryMutationContext(ctx, payload.ProjectID, payload.ServerURL)
	if err != nil {
		return err
	}

	updated, err := telemetryrepo.New(s.chConn).UpdateShadowMCPInventoryURLNameOverride(ctx, telemetryrepo.UpdateShadowMCPInventoryURLNameOverrideParams{
		GramProjectID:      projectID.String(),
		CanonicalServerURL: inventoryURL.CanonicalURL,
		ServerNameOverride: strings.TrimSpace(payload.Name),
		UpdatedAt:          time.Now(),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "update shadow mcp inventory server name").LogError(ctx, s.logger)
	}
	if !updated {
		return oops.E(oops.CodeNotFound, nil, "shadow mcp inventory url not found").LogError(ctx, s.logger)
	}
	return nil
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

func (s *Service) ResolveShadowMCPInventoryRequest(ctx context.Context, payload *gen.ResolveShadowMCPInventoryRequestPayload) (*gen.ShadowMCPInventoryURLState, error) {
	ac, projectID, inventoryURL, err := s.shadowMCPInventoryMutationContext(ctx, payload.ProjectID, payload.ServerURL)
	if err != nil {
		return nil, err
	}
	decision := strings.TrimSpace(string(payload.Decision))
	switch decision {
	case shadowMCPInventoryDecisionAllow, shadowMCPInventoryDecisionDeny:
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid shadow mcp inventory request decision")
	}
	// Policy ids are validated lazily: an allow decision on an allow_all
	// policy edits the blocked list and needs no policy selection.
	policyIDs, err := shadowMCPInventoryPolicyIDs(payload.PolicyIds, false)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin shadow mcp inventory request resolution").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	var policyAudiences map[string][]urn.Principal
	if decision == shadowMCPInventoryDecisionAllow {
		blockingPolicies, err := s.shadowMCPInventoryBlockingPolicies(ctx, dbtx, projectID)
		if err != nil {
			return nil, err
		}
		if allowAllPolicy := shadowMCPInventoryAllowAllPolicy(blockingPolicies); allowAllPolicy != nil {
			// Approval under allow_all unblocks the server for the whole
			// project: revoke the URL's risk_policy:block grant instead of
			// minting bypass grants, and resolve the pending requests against
			// that policy with no granted principals.
			if err := policybypass.RevokePolicyURL(ctx, dbtx, ac.ActiveOrganizationID, authz.ScopeRiskPolicyBlock, allowAllPolicy.ID.String(), inventoryURL.CanonicalURL); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "unblock shadow mcp server").LogError(ctx, s.logger)
			}
			policyIDs = []string{allowAllPolicy.ID.String()}
		} else {
			if len(policyIDs) == 0 {
				return nil, oops.E(oops.CodeBadRequest, nil, "at least one policy id is required")
			}
			policyAudiences, err = s.replaceShadowMCPInventoryURLBypassGrants(ctx, dbtx, ac.ActiveOrganizationID, projectID, inventoryURL.CanonicalURL, policyIDs)
			if err != nil {
				return nil, err
			}
		}
	}

	if err := s.resolveShadowMCPInventoryURLRequests(ctx, dbtx, projectID, inventoryURL.CanonicalURL, decision, ac.UserID, policyIDs, policyAudiences); err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp inventory request resolution").LogError(ctx, s.logger)
	}

	return s.shadowMCPInventoryURLState(ctx, ac.ActiveOrganizationID, projectID, inventoryURL.CanonicalURL)
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

func shadowMCPInventoryServerSlug(canonicalURL string) string {
	return shadowmcp.ServerSlug(canonicalURL)
}

func shadowMCPInventorySlugHash(serverSlug string) string {
	separator := strings.LastIndexByte(serverSlug, '-')
	if separator < 1 || len(serverSlug)-separator-1 != 8 {
		return ""
	}

	hash := serverSlug[separator+1:]
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}

	return hash
}

func shadowMCPInventoryURLForSlug(ctx context.Context, chRepo *telemetryrepo.Queries, projectID string, serverSlug string) (*telemetryrepo.ShadowMCPInventoryURLRow, error) {
	hash := shadowMCPInventorySlugHash(serverSlug)
	if hash == "" {
		return nil, nil
	}

	rows, err := chRepo.ListShadowMCPInventoryURLsBySlugHash(ctx, telemetryrepo.ListShadowMCPInventoryURLsBySlugHashParams{
		GramProjectID: projectID,
		SlugHash:      hash,
	})
	if err != nil {
		return nil, fmt.Errorf("listing shadow mcp inventory urls by slug hash: %w", err)
	}

	for _, row := range rows {
		if shadowMCPInventoryServerSlug(row.CanonicalServerURL) == serverSlug {
			return &row, nil
		}
	}

	return nil, nil
}

func (s *Service) shadowMCPInventoryMutationContext(ctx context.Context, rawProjectID string, rawServerURL string) (*contextvalues.AuthContext, uuid.UUID, shadowmcp.InventoryURL, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, uuid.UUID{}, shadowmcp.InventoryURL{}, err
	}

	projectID, err := uuid.Parse(rawProjectID)
	if err != nil {
		return nil, uuid.UUID{}, shadowmcp.InventoryURL{}, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, s.logger)
	}
	if err := s.requireProjectInOrganization(ctx, ac.ActiveOrganizationID, projectID); err != nil {
		return nil, uuid.UUID{}, shadowmcp.InventoryURL{}, err
	}

	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(rawServerURL)
	if !ok {
		return nil, uuid.UUID{}, shadowmcp.InventoryURL{}, oops.E(oops.CodeBadRequest, nil, "invalid shadow mcp server url").LogError(ctx, s.logger)
	}

	return ac, projectID, inventoryURL, nil
}

func shadowMCPInventoryPolicyIDs(rawPolicyIDs []string, requireAny bool) ([]string, error) {
	policyIDs := make([]string, 0, len(rawPolicyIDs))
	for _, rawPolicyID := range rawPolicyIDs {
		policyID := strings.TrimSpace(rawPolicyID)
		if policyID == "" {
			continue
		}
		if _, err := uuid.Parse(policyID); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid policy id")
		}
		policyIDs = append(policyIDs, policyID)
	}
	slices.Sort(policyIDs)
	policyIDs = slices.Compact(policyIDs)
	if requireAny && len(policyIDs) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one policy id is required")
	}
	return policyIDs, nil
}

func (s *Service) replaceShadowMCPInventoryURLBypassGrants(
	ctx context.Context,
	db riskrepo.DBTX,
	organizationID string,
	projectID uuid.UUID,
	canonicalURL string,
	selectedPolicyIDs []string,
) (map[string][]urn.Principal, error) {
	blockingPolicies, err := s.shadowMCPInventoryBlockingPolicies(ctx, db, projectID)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(selectedPolicyIDs))
	for _, policyID := range selectedPolicyIDs {
		if _, ok := blockingPolicies[policyID]; !ok {
			return nil, oops.E(oops.CodeBadRequest, nil, "policy must be an enabled blocking shadow mcp policy")
		}
		selected[policyID] = struct{}{}
	}

	shadowMCPPolicies, err := s.shadowMCPInventoryProjectPolicies(ctx, db, projectID)
	if err != nil {
		return nil, err
	}
	for _, policy := range shadowMCPPolicies {
		policyID := policy.ID.String()
		if err := policybypass.RevokePolicyURL(ctx, db, organizationID, authz.ScopeRiskPolicyBypass, policyID, canonicalURL); err != nil {
			return nil, fmt.Errorf("revoke shadow mcp inventory policy bypass grant: %w", err)
		}
	}

	audiences := make(map[string][]urn.Principal, len(selectedPolicyIDs))
	for _, policy := range blockingPolicies {
		policyID := policy.ID.String()
		if _, ok := selected[policyID]; !ok {
			continue
		}
		principals, err := shadowMCPInventoryPolicyAudiencePrincipals(ctx, db, organizationID, policyID)
		if err != nil {
			return nil, err
		}
		audiences[policyID] = principals
		if err := policybypass.ReplacePolicyURLAudience(ctx, db, organizationID, authz.ScopeRiskPolicyBypass, policyID, canonicalURL, principals); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "grant shadow mcp inventory policy bypass").LogError(ctx, s.logger)
		}
	}

	return audiences, nil
}

func (s *Service) shadowMCPInventoryBlockingPolicies(ctx context.Context, db riskrepo.DBTX, projectID uuid.UUID) (map[string]riskrepo.RiskPolicy, error) {
	rows, err := riskrepo.New(db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp policies").LogError(ctx, s.logger)
	}
	policies := make(map[string]riskrepo.RiskPolicy, len(rows))
	for _, row := range rows {
		if row.Action != "block" {
			continue
		}
		policies[row.ID.String()] = row
	}
	return policies, nil
}

// shadowMCPInventoryAllowAllPolicy returns the allow_all blocking policy when
// every enabled blocking policy declares the allow_all disposition. Mirrors
// the forURL semantics: with mixed legacy data, deny-by-default wins and this
// returns nil.
func shadowMCPInventoryAllowAllPolicy(blockingPolicies map[string]riskrepo.RiskPolicy) *riskrepo.RiskPolicy {
	var candidate *riskrepo.RiskPolicy
	for _, policy := range blockingPolicies {
		if !policy.ShadowMcpDisposition.Valid || policy.ShadowMcpDisposition.String != shadowmcp.DispositionAllowAll {
			return nil
		}
		candidate = &policy
	}
	return candidate
}

func (s *Service) shadowMCPInventoryProjectPolicies(ctx context.Context, db riskrepo.DBTX, projectID uuid.UUID) ([]riskrepo.RiskPolicy, error) {
	rows, err := riskrepo.New(db).ListRiskPolicies(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp project policies").LogError(ctx, s.logger)
	}
	policies := make([]riskrepo.RiskPolicy, 0, len(rows))
	for _, row := range rows {
		if !slices.Contains(row.Sources, "shadow_mcp") {
			continue
		}
		policies = append(policies, row)
	}
	return policies, nil
}

func shadowMCPInventoryPolicyAudiencePrincipals(ctx context.Context, db riskrepo.DBTX, organizationID string, policyID string) ([]urn.Principal, error) {
	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyEvaluate,
		ResourceID:     policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("list shadow mcp policy audience grants: %w", err)
	}

	principals := make([]urn.Principal, 0, len(grants))
	for _, grant := range grants {
		if !maps.Equal(grant.Selector, authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)) {
			continue
		}
		principal, err := urn.ParsePrincipal(grant.PrincipalUrn)
		if err != nil {
			return nil, fmt.Errorf("parse shadow mcp policy audience principal: %w", err)
		}
		principals = append(principals, principal)
	}
	if len(principals) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "policy audience is empty")
	}
	return principals, nil
}

func (s *Service) resolveShadowMCPInventoryURLRequests(
	ctx context.Context,
	db riskrepo.DBTX,
	projectID uuid.UUID,
	canonicalURL string,
	decision string,
	decidedBy string,
	selectedPolicyIDs []string,
	policyAudiences map[string][]urn.Principal,
) error {
	blockingPolicies, err := s.shadowMCPInventoryBlockingPolicies(ctx, db, projectID)
	if err != nil {
		return err
	}
	selected := make(map[string]struct{}, len(selectedPolicyIDs))
	for _, policyID := range selectedPolicyIDs {
		selected[policyID] = struct{}{}
	}

	q := riskrepo.New(db)
	requests, err := q.ListRiskPolicyBypassRequests(ctx, riskrepo.ListRiskPolicyBypassRequestsParams{
		ProjectID:        projectID,
		RiskPolicyID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Status:           conv.ToPGText(shadowMCPInventoryBypassStatusRequested),
		RequesterUserIds: nil,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory requests").LogError(ctx, s.logger)
	}
	for _, request := range requests {
		policyID := request.RiskPolicyID.String()
		if _, ok := blockingPolicies[policyID]; !ok {
			continue
		}
		if conv.FromPGTextOrEmpty[string](request.TargetKind) != shadowMCPInventoryBypassTargetKind {
			continue
		}
		dimensions, err := shadowMCPInventoryBypassDimensions(request.TargetDimensions)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "parse shadow mcp inventory request dimensions").LogError(ctx, s.logger)
		}
		if dimensions[authz.SelectorKeyServerURL] != canonicalURL {
			continue
		}

		status := shadowMCPInventoryBypassStatusDenied
		grantedPrincipalURNs := []string{}
		if decision == shadowMCPInventoryDecisionAllow {
			if _, ok := selected[policyID]; ok {
				status = shadowMCPInventoryBypassStatusApproved
				grantedPrincipalURNs = shadowMCPInventoryPrincipalStrings(policyAudiences[policyID])
			}
		}
		if _, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, riskrepo.UpdateRiskPolicyBypassRequestStatusParams{
			Status:               status,
			DecidedBy:            conv.ToPGText(decidedBy),
			GrantedPrincipalUrns: grantedPrincipalURNs,
			ID:                   request.ID,
			ProjectID:            projectID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "resolve shadow mcp inventory request").LogError(ctx, s.logger)
		}
	}
	return nil
}

func shadowMCPInventoryPrincipalStrings(principals []urn.Principal) []string {
	values := make([]string, 0, len(principals))
	for _, principal := range principals {
		values = append(values, principal.String())
	}
	slices.Sort(values)
	return slices.Compact(values)
}

func (s *Service) shadowMCPInventoryURLState(ctx context.Context, organizationID string, projectID uuid.UUID, canonicalURL string) (*gen.ShadowMCPInventoryURLState, error) {
	state, err := s.shadowMCPInventoryPolicyState(ctx, organizationID, projectID, []string{canonicalURL})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load shadow mcp inventory url state").LogError(ctx, s.logger)
	}
	return buildShadowMCPInventoryURLState(state.forURL(canonicalURL)), nil
}

func buildShadowMCPInventoryURLState(rowState shadowMCPInventoryRowState) *gen.ShadowMCPInventoryURLState {
	return &gen.ShadowMCPInventoryURLState{
		Access:           rowState.Access,
		RequestCount:     rowState.RequestCount,
		LatestRequest:    rowState.LatestRequest,
		ApprovalRequest:  rowState.ApprovalRequest,
		AllowedPolicyIds: rowState.AllowedPolicyIDs,
		BlockedPolicyIds: rowState.BlockedPolicyIDs,
	}
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
	// hasBlockAllPolicy is set when any enabled blocking policy denies by
	// default. When only allow_all blocking policies exist, rows are blocked
	// solely by risk_policy:block grants (blockedPolicyIDs).
	hasBlockAllPolicy bool
	allowedPolicyIDs  map[string][]string
	blockedPolicyIDs  map[string][]string
	requestsByURL     map[string]shadowMCPInventoryRequestState
	approvalsByURL    map[string]*gen.ShadowMCPInventoryApprovalRequest
}

type shadowMCPInventoryRowState struct {
	Access           string
	RequestCount     int
	LatestRequest    *gen.ShadowMCPInventoryRequestSummary
	ApprovalRequest  *gen.ShadowMCPInventoryApprovalRequest
	AllowedPolicyIDs []string
	BlockedPolicyIDs []string
}

type shadowMCPInventoryRequestState struct {
	Count  int
	Latest *gen.ShadowMCPInventoryRequestSummary
	At     time.Time
}

func (s *Service) shadowMCPInventoryPolicyState(ctx context.Context, organizationID string, projectID uuid.UUID, canonicalURLs []string) (shadowMCPInventoryPolicyState, error) {
	state := shadowMCPInventoryPolicyState{
		hasBlockingPolicy: false,
		hasBlockAllPolicy: false,
		allowedPolicyIDs:  map[string][]string{},
		blockedPolicyIDs:  map[string][]string{},
		requestsByURL:     map[string]shadowMCPInventoryRequestState{},
		approvalsByURL:    map[string]*gen.ShadowMCPInventoryApprovalRequest{},
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

	// Approval requests track servers independently of which policies exist,
	// so they join onto every row, not just policy-covered ones. target_key
	// for a server_url request is the same canonical URL the inventory keys
	// on.
	approvalRows, err := mcpapprovalrepo.New(s.db).ListApprovalRequestsByTargetKeys(ctx, mcpapprovalrepo.ListApprovalRequestsByTargetKeysParams{
		ProjectID:  projectID,
		TargetKeys: slices.Sorted(maps.Keys(canonicalURLSet)),
	})
	if err != nil {
		return state, fmt.Errorf("listing approval requests for shadow mcp inventory: %w", err)
	}
	for _, row := range approvalRows {
		state.approvalsByURL[row.TargetKey] = &gen.ShadowMCPInventoryApprovalRequest{
			ID:                row.ID.String(),
			Status:            row.Status,
			RequesterCount:    int(row.RequesterCount),
			EvidenceChangedAt: conv.PtrEmpty(conv.FromPGTimestamptz(row.EvidenceChangedAt)),
		}
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

		// allow_all policies carry their exceptions as risk_policy:block
		// grants and never have per-URL bypass grants, so only the block
		// grants are listed for them.
		if policy.ShadowMcpDisposition.Valid && policy.ShadowMcpDisposition.String == shadowmcp.DispositionAllowAll {
			blockGrants, err := authz.ListGrantsForResource(ctx, s.db, authz.Resource{
				OrganizationID: organizationID,
				Scope:          authz.ScopeRiskPolicyBlock,
				ResourceID:     policyID,
			})
			if err != nil {
				return state, fmt.Errorf("listing block grants for shadow mcp policy: %w", err)
			}
			for _, grant := range blockGrants {
				serverURL := grant.Selector[authz.SelectorKeyServerURL]
				if _, ok := canonicalURLSet[serverURL]; !ok {
					continue
				}
				state.blockedPolicyIDs[serverURL] = append(state.blockedPolicyIDs[serverURL], policyID)
			}
			continue
		}
		state.hasBlockAllPolicy = true

		grants, err := authz.ListGrantsForResource(ctx, s.db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		})
		if err != nil {
			return state, fmt.Errorf("listing grants for shadow mcp policy: %w", err)
		}
		for _, grant := range grants {
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
		ProjectID:        projectID,
		RiskPolicyID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Status:           conv.ToPGText(shadowMCPInventoryBypassStatusRequested),
		RequesterUserIds: nil,
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
	for serverURL, policyIDs := range state.blockedPolicyIDs {
		slices.Sort(policyIDs)
		state.blockedPolicyIDs[serverURL] = slices.Compact(policyIDs)
	}

	return state, nil
}

func (s shadowMCPInventoryPolicyState) forURL(canonicalURL string) shadowMCPInventoryRowState {
	requestState := s.requestsByURL[canonicalURL]
	allowedPolicyIDs := s.allowedPolicyIDs[canonicalURL]
	onBlockedList := len(s.blockedPolicyIDs[canonicalURL]) > 0
	access := shadowMCPInventoryAccessNone
	switch {
	case len(allowedPolicyIDs) > 0:
		access = shadowMCPInventoryAccessAllowed
	case s.hasBlockAllPolicy || onBlockedList:
		access = shadowMCPInventoryAccessBlocked
	case s.hasBlockingPolicy:
		// Only allow_all blocking policies exist and this URL is not on any
		// blocked list: the default disposition permits it.
		access = shadowMCPInventoryAccessAllowed
	}

	return shadowMCPInventoryRowState{
		Access:           access,
		RequestCount:     requestState.Count,
		LatestRequest:    requestState.Latest,
		ApprovalRequest:  s.approvalsByURL[canonicalURL],
		AllowedPolicyIDs: allowedPolicyIDs,
		BlockedPolicyIDs: s.blockedPolicyIDs[canonicalURL],
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

func buildShadowMCPInventoryServer(row telemetryrepo.ShadowMCPInventoryURLRow, usage telemetryrepo.ShadowMCPInventoryUsageRow, rowState shadowMCPInventoryRowState, targetKind string) *gen.ShadowMCPInventoryServer {
	var serverName *string
	serverNameValue := row.ServerNameOverride
	if serverNameValue == "" {
		serverNameValue = row.ServerName
	}
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
		ServerSlug:         shadowMCPInventoryServerSlug(row.CanonicalServerURL),
		URLHost:            row.URLHost,
		TargetKind:         conv.PtrEmpty(targetKind),
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
		ApprovalRequest:    rowState.ApprovalRequest,
		AllowedPolicyIds:   rowState.AllowedPolicyIDs,
		BlockedPolicyIds:   rowState.BlockedPolicyIDs,
	}
}

func buildShadowMCPInventoryUser(row telemetryrepo.ShadowMCPInventoryUserRow) *gen.ShadowMCPInventoryUser {
	sources := make([]*gen.ShadowMCPInventoryUserSource, 0, len(row.Sources))
	for _, source := range row.Sources {
		sources = append(sources, &gen.ShadowMCPInventoryUserSource{
			Source:           source.Source,
			ObservedUseCount: shadowMCPInventoryCount(source.CallCount),
		})
	}

	return &gen.ShadowMCPInventoryUser{
		UserKey:          row.UserKey,
		Name:             nil,
		Email:            conv.PtrEmpty(row.UserEmail),
		LastCalled:       formatTimeValue(row.LastCalled),
		ObservedUseCount: shadowMCPInventoryCount(row.CallCount),
		Sources:          sources,
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
