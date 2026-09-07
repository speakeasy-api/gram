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

	shadowMCPInventoryAccessNone       = "none"
	shadowMCPInventoryAccessAllowed    = "allowed"
	shadowMCPInventoryAccessBlocked    = "blocked"
	shadowMCPInventoryAccessRestricted = "restricted"

	// shadowMCPInventoryAudienceEveryone is the risk_policies.audience_type
	// value for a policy that applies to every user, as opposed to "targeted",
	// which applies to a named subset. A targeted blocking policy blocks a
	// server for some users but not all, which the inventory surfaces as
	// restricted rather than blocked.
	shadowMCPInventoryAudienceEveryone = "everyone"

	shadowMCPInventoryBypassStatusRequested  = "requested"
	shadowMCPInventoryBypassStatusApproved   = "approved"
	shadowMCPInventoryBypassStatusDenied     = "denied"
	shadowMCPInventoryBypassStatusRevoked    = "revoked"
	shadowMCPInventoryBypassStatusSuperseded = "superseded"
	shadowMCPInventoryBypassTargetKind       = "shadow_mcp_server"

	shadowMCPInventoryDecisionAllow = "allow"
	shadowMCPInventoryDecisionDeny  = "deny"

	shadowMCPTargetKindServerURL    = "server_url"
	shadowMCPTargetKindStdioCommand = "stdio_command"

	shadowMCPAccessStateAllowed    = "allowed"
	shadowMCPAccessStateRestricted = "restricted"
	shadowMCPAccessStateBlocked    = "blocked"
	shadowMCPAccessStateUnenforced = "unenforced"

	shadowMCPAccessReachEveryone = "everyone"
	shadowMCPAccessReachSelected = "selected"
	shadowMCPAccessReachSome     = "some"
	shadowMCPAccessReachNone     = "none"

	shadowMCPAccessDefaultDeny  = "deny"
	shadowMCPAccessDefaultAllow = "allow"
	shadowMCPAccessDefaultNone  = "none"

	shadowMCPAccessCoverageFull    = "full"
	shadowMCPAccessCoveragePartial = "partial"
	shadowMCPAccessCoverageNone    = "none"
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
			OrganizationID:      "",
			UserKeys:            nil,
			From:                nil,
			To:                  nil,
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

	// Every request-only target key joins the policy-state universe, stdio
	// included: their reviews come back through the same approvals join, and
	// a page whose only rows are stdio must still load the policy set — the
	// posture of one row must not depend on which other rows share the page.
	policyURLs := shadowMCPInventoryCanonicalURLs(inventoryRows)
	for _, request := range requestOnly {
		policyURLs = append(policyURLs, request.TargetKey)
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

// standingDecisionValue derives the standing-decision field: the latest
// decision counts whatever the lifecycle status says, except superseded,
// where it was explicitly displaced.
func standingDecisionValue(status string, latestDecision string) *string {
	if status == shadowMCPInventoryBypassStatusSuperseded || latestDecision == "" {
		return nil
	}
	return conv.PtrEmpty(latestDecision)
}

// buildShadowMCPRequestOnlyServer synthesizes a servers-table row from a
// review with no telemetry behind it: zero usage, zero seen-times (the
// never-observed sentinel), and the review carried as the row's approval
// state. Stdio commands have no URL host and no server page.
func buildShadowMCPRequestOnlyServer(request mcpapprovalrepo.ListApprovalRequestTargetsRow, policyState shadowMCPInventoryPolicyState) *gen.ShadowMCPInventoryServer {
	targetKind := shadowMCPTargetKindStdioCommand
	urlHost := ""
	// forURL works for stdio keys too: their per-URL grant lookups come back
	// empty, leaving the posture-only verdict (what the enabled policies do
	// to a target no rule names), which is what an unresolved local command
	// actually faces.
	rowState := policyState.forURL(request.TargetKey)
	if request.TargetKind == shadowMCPTargetKindServerURL {
		targetKind = shadowMCPTargetKindServerURL
		inventoryURL, _ := shadowmcp.CanonicalizeInventoryURL(request.TargetKey)
		urlHost = inventoryURL.URLHost
	} else {
		// Wire parity: the legacy access field for stdio rows has always
		// read none; the summary carries the honest posture.
		rowState.Access = shadowMCPInventoryAccessNone
		rowState.RequestCount = 0
		rowState.LatestRequest = nil
	}
	// The review is authoritative for its own row whether or not the batched
	// join saw it (the join only covers server_url targets).
	rowState.ApprovalRequest = &gen.ShadowMCPInventoryApprovalRequest{
		ID:                request.ID.String(),
		Status:            request.Status,
		StandingDecision:  standingDecisionValue(request.Status, request.LatestDecision),
		RequesterCount:    int(request.RequesterCount),
		EvidenceChangedAt: conv.PtrEmpty(conv.FromPGTimestamptz(request.EvidenceChangedAt)),
	}
	// Same authority for the verdict's decision, for callers whose batched
	// join did not include this key.
	if rowState.Summary.Decision == nil &&
		(request.Status == shadowMCPInventoryBypassStatusApproved || request.Status == shadowMCPInventoryBypassStatusDenied) {
		rowState.Summary.Decision = conv.PtrEmpty(request.Status)
	}
	// Decisions on stdio targets are recorded without writing enforcement —
	// the grant writer only acts on server_url targets — so whatever the
	// posture, no mechanism carries the decision. Coverage must say so
	// rather than let "partial" claim a delivery that does not exist.
	if request.TargetKind != shadowMCPTargetKindServerURL {
		rowState.Summary.DecisionCoverage = shadowMCPAccessCoverageNone
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
		OrganizationID:      "",
		UserKeys:            nil,
		From:                nil,
		To:                  nil,
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

// ListShadowMCPInventoryServersForUser answers the question the inventory
// table cannot: which shadow MCP servers one person reached. The table is
// keyed by URL with no user column, so the set of servers is derived from that
// person's telemetry and then enriched with the same policy state the
// project-wide listing shows.
//
// Unlike the project-wide listing this is not cursor-paginated: one person
// reaches a handful of servers, and a bounded page keeps the derived URL set
// and the policy state it feeds consistent within a single response.
func (s *Service) ListShadowMCPInventoryServersForUser(ctx context.Context, payload *gen.ListShadowMCPInventoryServersForUserPayload) (*gen.ListShadowMCPInventoryResult, error) {
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

	userKeys := make([]string, 0, len(payload.UserKeys))
	for _, key := range payload.UserKeys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			userKeys = append(userKeys, trimmed)
		}
	}
	if len(userKeys) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "at least one user key is required")
	}

	limit, err := shadowMCPInventoryLimit(payload.Limit)
	if err != nil {
		return nil, err
	}

	// The window is a filter, not a required frame: with neither bound this
	// still answers over the whole history.
	from, to, err := conv.ParseOptionalTimeWindow(payload.From, payload.To)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogError(ctx, s.logger)
	}

	chRepo := telemetryrepo.New(s.chConn)
	usageRows, err := chRepo.ListShadowMCPInventoryUsage(ctx, telemetryrepo.ListShadowMCPInventoryUsageParams{
		// Gated, not the raw org id: the fold is behind a rollout flag, and
		// this endpoint must fall under the same switch as every other folded
		// read. With the fold off the email leg drops and matching falls back
		// to the user id, which returns less rather than something wrong.
		OrganizationID:      s.canonicalFoldOrg(ctx, ac.ActiveOrganizationID),
		GramProjectID:       projectID.String(),
		CanonicalServerURLs: nil,
		UserKeys:            userKeys,
		Limit:               shadowMCPInventoryUsageTraceLimit,
		From:                from,
		To:                  to,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory usage for user").LogError(ctx, s.logger)
	}

	// Most-recently called first: the page bound is what the caller sees, so
	// it has to keep the servers that matter rather than an arbitrary slice.
	slices.SortStableFunc(usageRows, func(left, right telemetryrepo.ShadowMCPInventoryUsageRow) int {
		switch {
		case left.LastCalled == nil && right.LastCalled == nil:
			return 0
		case left.LastCalled == nil:
			return 1
		case right.LastCalled == nil:
			return -1
		default:
			return right.LastCalled.Compare(*left.LastCalled)
		}
	})
	if len(usageRows) > limit {
		usageRows = usageRows[:limit]
	}

	canonicalURLs := make([]string, 0, len(usageRows))
	for _, usage := range usageRows {
		canonicalURLs = append(canonicalURLs, usage.CanonicalServerURL)
	}

	// The stored inventory row is what carries an admin's rename and the
	// project-wide first/last seen. Deriving the server set from one person's
	// telemetry must not cost either, or the same server would read
	// differently here than on the inventory page.
	inventoryRows, err := chRepo.ListShadowMCPInventoryURLsByCanonicalURLs(ctx, telemetryrepo.ListShadowMCPInventoryURLsByCanonicalURLsParams{
		GramProjectID:       projectID.String(),
		CanonicalServerURLs: canonicalURLs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp inventory metadata for user").LogError(ctx, s.logger)
	}
	inventoryByURL := make(map[string]telemetryrepo.ShadowMCPInventoryURLRow, len(inventoryRows))
	for _, row := range inventoryRows {
		inventoryByURL[row.CanonicalServerURL] = row
	}

	policyState, err := s.shadowMCPInventoryPolicyState(ctx, ac.ActiveOrganizationID, projectID, canonicalURLs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load shadow mcp inventory policy state").LogError(ctx, s.logger)
	}

	servers := make([]*gen.ShadowMCPInventoryServer, 0, len(usageRows))
	for _, usage := range usageRows {
		// Start from the stored row so the rename and the project-wide seen
		// range survive; fall back to what telemetry reported for servers the
		// inventory has not recorded yet.
		row, stored := inventoryByURL[usage.CanonicalServerURL]
		row.CanonicalServerURL = usage.CanonicalServerURL
		if row.ServerName == "" {
			row.ServerName = usage.ServerName
		}
		if !stored {
			if usage.FirstCalled != nil {
				row.FirstSeen = *usage.FirstCalled
			}
			if usage.LastCalled != nil {
				row.LastSeen = *usage.LastCalled
			}
		}
		if usage.LastCalled != nil {
			row.LastCalledUnixNano = usage.LastCalled.UTC().UnixNano()
		}
		if row.URLHost == "" {
			if invURL, ok := shadowmcp.CanonicalizeInventoryURL(usage.CanonicalServerURL); ok {
				row.URLHost = invURL.URLHost
			}
		}
		servers = append(servers, buildShadowMCPInventoryServer(row, usage, policyState.forURL(usage.CanonicalServerURL), shadowMCPTargetKindServerURL))
	}

	return &gen.ListShadowMCPInventoryResult{
		Servers:    servers,
		NextCursor: nil,
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
		ProjectID:    projectID,
		RiskPolicyID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Status:       conv.ToPGText(shadowMCPInventoryBypassStatusRequested),
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
		AccessSummary:    rowState.Summary,
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

	// hasEveryoneBlockAllPolicy is set when a deny-by-default policy applies to
	// every user (audience_type everyone). It separates a project-wide block
	// from one a targeted policy imposes on a subset: without an everyone-scoped
	// source, a deny-by-default block is restricted (blocked for some) rather
	// than blocked.
	hasEveryoneBlockAllPolicy bool

	allowedPolicyIDs map[string][]string
	blockedPolicyIDs map[string][]string

	// everyoneBlockedURLs holds the URLs an allow_all policy that applies to
	// every user blocks via a risk_policy:block grant. A URL blocked only by
	// targeted allow_all policies is restricted, not blocked.
	everyoneBlockedURLs map[string]struct{}

	// blockAllPolicyAudiences maps each deny-by-default policy to its
	// audience principal URNs (the evaluate grants). A bypass grant only
	// frees the users the policy would have blocked, so whether a URL is
	// allowed for everyone is a per-policy set question: do its bypass
	// principals cover this policy's audience? Matching the literal
	// all-users principal is not enough — approving a targeted policy's URL
	// writes the audience's own principals, which covers everyone the
	// policy ever blocked — and any-policy aggregation is too much, since a
	// grant on one policy says nothing about another still blocking.
	blockAllPolicyAudiences map[string][]string

	// bypassPrincipalsByURL holds, per URL and per deny-by-default policy,
	// the principal URNs its bypass grants name.
	bypassPrincipalsByURL map[string]map[string]map[string]struct{}

	requestsByURL  map[string]shadowMCPInventoryRequestState
	approvalsByURL map[string]*gen.ShadowMCPInventoryApprovalRequest
}

type shadowMCPInventoryRowState struct {
	Access           string
	Summary          *gen.ShadowMCPAccessSummary
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
		hasBlockingPolicy:         false,
		hasBlockAllPolicy:         false,
		hasEveryoneBlockAllPolicy: false,
		allowedPolicyIDs:          map[string][]string{},
		blockedPolicyIDs:          map[string][]string{},
		everyoneBlockedURLs:       map[string]struct{}{},
		blockAllPolicyAudiences:   map[string][]string{},
		bypassPrincipalsByURL:     map[string]map[string]map[string]struct{}{},
		requestsByURL:             map[string]shadowMCPInventoryRequestState{},
		approvalsByURL:            map[string]*gen.ShadowMCPInventoryApprovalRequest{},
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
			StandingDecision:  standingDecisionValue(row.Status, row.LatestDecision),
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
			everyoneAudience := policy.AudienceType == shadowMCPInventoryAudienceEveryone
			for _, grant := range blockGrants {
				serverURL := grant.Selector[authz.SelectorKeyServerURL]
				if _, ok := canonicalURLSet[serverURL]; !ok {
					continue
				}
				state.blockedPolicyIDs[serverURL] = append(state.blockedPolicyIDs[serverURL], policyID)
				if everyoneAudience {
					state.everyoneBlockedURLs[serverURL] = struct{}{}
				}
			}
			continue
		}
		state.hasBlockAllPolicy = true
		if policy.AudienceType == shadowMCPInventoryAudienceEveryone {
			state.hasEveryoneBlockAllPolicy = true
		}

		audienceGrants, err := authz.ListGrantsForResource(ctx, s.db, authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policyID,
		})
		if err != nil {
			return state, fmt.Errorf("listing audience grants for shadow mcp policy: %w", err)
		}
		audience := make([]string, 0, len(audienceGrants))
		baseSelector := authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)
		for _, grant := range audienceGrants {
			// Only base-selector grants are the audience; a grant carrying
			// extra selector keys scopes something narrower, and counting it
			// would inflate the audience a bypass set must cover.
			if !maps.Equal(grant.Selector, baseSelector) {
				continue
			}
			audience = append(audience, grant.PrincipalUrn)
		}
		state.blockAllPolicyAudiences[policyID] = audience

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
			byPolicy := state.bypassPrincipalsByURL[serverURL]
			if byPolicy == nil {
				byPolicy = map[string]map[string]struct{}{}
				state.bypassPrincipalsByURL[serverURL] = byPolicy
			}
			principals := byPolicy[policyID]
			if principals == nil {
				principals = map[string]struct{}{}
				byPolicy[policyID] = principals
			}
			principals[grant.PrincipalUrn] = struct{}{}
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
	_, blockedForEveryoneList := s.everyoneBlockedURLs[canonicalURL]
	// A block sourced from an everyone-audience policy stops every user; one
	// sourced only from targeted policies stops a subset, which reads as
	// restricted rather than blocked.
	blockedForEveryone := s.hasEveryoneBlockAllPolicy || blockedForEveryoneList
	blockedForSome := s.hasBlockAllPolicy || onBlockedList
	access := shadowMCPInventoryAccessNone
	allowedForEveryone := s.bypassCoversEveryBlockAllAudience(canonicalURL)
	switch {
	case len(allowedPolicyIDs) > 0 && allowedForEveryone:
		access = shadowMCPInventoryAccessAllowed
	case len(allowedPolicyIDs) > 0:
		// Bypass grants exist but leave part of some policy's audience
		// blocked: a scoped approval lets its people through while the
		// policy still blocks the rest. The same in-between state as a
		// targeted block, so it reads as restricted rather than allowed.
		access = shadowMCPInventoryAccessRestricted
	case blockedForEveryone:
		access = shadowMCPInventoryAccessBlocked
	case blockedForSome:
		access = shadowMCPInventoryAccessRestricted
	case s.hasBlockingPolicy:
		// Only allow_all blocking policies exist and this URL is not on any
		// blocked list: the default disposition permits it.
		access = shadowMCPInventoryAccessAllowed
	}

	return shadowMCPInventoryRowState{
		Access:           access,
		Summary:          s.summaryForURL(canonicalURL, access, allowedForEveryone),
		RequestCount:     requestState.Count,
		LatestRequest:    requestState.Latest,
		ApprovalRequest:  s.approvalsByURL[canonicalURL],
		AllowedPolicyIDs: allowedPolicyIDs,
		BlockedPolicyIDs: s.blockedPolicyIDs[canonicalURL],
	}
}

// bypassCoversEveryBlockAllAudience reports whether the URL's bypass grants
// free everyone each deny-by-default policy would otherwise block. Per
// policy: a grant set containing the all-users principal, or covering every
// audience principal, means nobody that policy blocked is still blocked.
// Users outside a targeted policy's audience were never blocked, so covering
// the audience is covering everyone. Every deny-by-default policy must be
// covered — a grant on one says nothing about another still blocking.
//
// The one reach this cannot see is a role principal whose membership happens
// to be the whole organization: that still reads as a subset, since grants
// are compared as principal sets, not expanded memberships.
func (s shadowMCPInventoryPolicyState) bypassCoversEveryBlockAllAudience(canonicalURL string) bool {
	if len(s.blockAllPolicyAudiences) == 0 {
		return false
	}
	byPolicy := s.bypassPrincipalsByURL[canonicalURL]
	allUsers := authz.AllUsersPrincipal().String()
	for policyID, audience := range s.blockAllPolicyAudiences {
		principals := byPolicy[policyID]
		if _, ok := principals[allUsers]; ok {
			continue
		}
		if len(audience) == 0 {
			// No audience grants at all is unexpected for an enabled policy;
			// treat it as uncovered rather than trivially covered.
			return false
		}
		for _, urn := range audience {
			if _, ok := principals[urn]; !ok {
				return false
			}
		}
	}
	return true
}

// summaryForURL compresses the policy and grant state for one URL into the
// typed verdict the client renders from. The server owns this computation
// because it is the only place holding all the inputs: clients that
// re-derived enforcement from policy lists and review status each told a
// slightly different lie.
func (s shadowMCPInventoryPolicyState) summaryForURL(canonicalURL string, access string, allowedForEveryone bool) *gen.ShadowMCPAccessSummary {
	// The legacy access value and the summary state are the same partition
	// under different names: none meant "no blocking policy", which unenforced
	// says outright.
	state := access
	if access == shadowMCPInventoryAccessNone {
		state = shadowMCPAccessStateUnenforced
	}

	allowedFor := shadowMCPAccessReachNone
	if len(s.allowedPolicyIDs[canonicalURL]) > 0 {
		allowedFor = shadowMCPAccessReachSelected
		if allowedForEveryone {
			allowedFor = shadowMCPAccessReachEveryone
		}
	}

	// Explicit blocks only: the deny-by-default posture is blockingDefault's
	// to report, so "Blocked by policy" and "Blocked by rule" stay separable.
	blockedFor := shadowMCPAccessReachNone
	if _, everyone := s.everyoneBlockedURLs[canonicalURL]; everyone {
		blockedFor = shadowMCPAccessReachEveryone
	} else if len(s.blockedPolicyIDs[canonicalURL]) > 0 || (s.hasBlockAllPolicy && !s.hasEveryoneBlockAllPolicy) {
		blockedFor = shadowMCPAccessReachSome
	}

	blockingDefault := shadowMCPAccessDefaultNone
	switch {
	case s.hasEveryoneBlockAllPolicy:
		blockingDefault = shadowMCPAccessDefaultDeny
	case s.hasBlockingPolicy:
		blockingDefault = shadowMCPAccessDefaultAllow
	}

	var decision *string
	coverage := shadowMCPAccessCoverageNone
	if approval := s.approvalsByURL[canonicalURL]; approval != nil {
		switch approval.Status {
		case shadowMCPInventoryBypassStatusApproved, shadowMCPInventoryBypassStatusDenied:
			decision = conv.PtrEmpty(approval.Status)
		}
	}
	// Coverage is how much of the recorded decision enforcement delivers.
	// An approval is fully carried while the grants it wrote survive (its
	// blast radius may be scoped — that is the decision as recorded, not a
	// shortfall) unless an explicit block overrides them; a denial is fully
	// carried only when the result is a project-wide block, and partially
	// when only a targeted policy enforces it. With no blocking policy there
	// is nothing to carry a decision at all.
	if decision != nil && s.hasBlockingPolicy {
		coverage = shadowMCPAccessCoveragePartial
		switch *decision {
		case shadowMCPInventoryBypassStatusApproved:
			// Full while the approval's grants survive and no explicit block
			// rule overrides them. The targeted deny-by-default posture is
			// not an override — the approval was recorded against it.
			if allowedFor != shadowMCPAccessReachNone && len(s.blockedPolicyIDs[canonicalURL]) == 0 {
				coverage = shadowMCPAccessCoverageFull
			}
		case shadowMCPInventoryBypassStatusDenied:
			if state == shadowMCPAccessStateBlocked {
				coverage = shadowMCPAccessCoverageFull
			}
		}
	}

	return &gen.ShadowMCPAccessSummary{
		State:            state,
		AllowedFor:       allowedFor,
		BlockedFor:       blockedFor,
		BlockingDefault:  blockingDefault,
		Decision:         decision,
		DecisionCoverage: coverage,
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
		AccessSummary:      rowState.Summary,
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
