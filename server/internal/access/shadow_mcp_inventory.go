package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
		return nil, oops.E(oops.CodeNotFound, nil, "shadow mcp inventory url not found").LogError(ctx, s.logger)
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

	return buildShadowMCPInventoryServer(*inventoryRow, usageByURL[inventoryRow.CanonicalServerURL], policyState.forURL(inventoryRow.CanonicalServerURL)), nil
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

func (s *Service) UpsertShadowMCPInventoryPolicyBypass(ctx context.Context, payload *gen.UpsertShadowMCPInventoryPolicyBypassPayload) (*gen.ShadowMCPInventoryURLState, error) {
	ac, projectID, inventoryURL, err := s.shadowMCPInventoryMutationContext(ctx, payload.ProjectID, payload.ServerURL)
	if err != nil {
		return nil, err
	}
	policyIDs, err := shadowMCPInventoryPolicyIDs(payload.PolicyIds, true)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin shadow mcp inventory policy bypass upsert").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if _, err := s.replaceShadowMCPInventoryURLBypassGrants(ctx, dbtx, ac.ActiveOrganizationID, projectID, inventoryURL.CanonicalURL, policyIDs); err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp inventory policy bypass upsert").LogError(ctx, s.logger)
	}

	return s.shadowMCPInventoryURLState(ctx, ac.ActiveOrganizationID, projectID, inventoryURL.CanonicalURL)
}

func (s *Service) DeleteShadowMCPInventoryPolicyBypass(ctx context.Context, payload *gen.DeleteShadowMCPInventoryPolicyBypassPayload) (*gen.ShadowMCPInventoryURLState, error) {
	ac, projectID, inventoryURL, err := s.shadowMCPInventoryMutationContext(ctx, payload.ProjectID, payload.ServerURL)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin shadow mcp inventory policy bypass delete").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if _, err := s.replaceShadowMCPInventoryURLBypassGrants(ctx, dbtx, ac.ActiveOrganizationID, projectID, inventoryURL.CanonicalURL, nil); err != nil {
		return nil, err
	}
	if err := s.revokeShadowMCPInventoryURLRequests(ctx, dbtx, ac, projectID, inventoryURL.CanonicalURL); err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp inventory policy bypass delete").LogError(ctx, s.logger)
	}

	return s.shadowMCPInventoryURLState(ctx, ac.ActiveOrganizationID, projectID, inventoryURL.CanonicalURL)
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
	policyIDs, err := shadowMCPInventoryPolicyIDs(payload.PolicyIds, decision == shadowMCPInventoryDecisionAllow)
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
		policyAudiences, err = s.replaceShadowMCPInventoryURLBypassGrants(ctx, dbtx, ac.ActiveOrganizationID, projectID, inventoryURL.CanonicalURL, policyIDs)
		if err != nil {
			return nil, err
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
	hash := sha256.Sum256([]byte(canonicalURL))
	hashSuffix := hex.EncodeToString(hash[:])[:8]

	label := strings.TrimPrefix(canonicalURL, "https://")
	label = strings.TrimPrefix(label, "http://")
	prefix := conv.URLToSlug(label)
	if prefix == "" {
		prefix = "server"
	}

	return prefix + "-" + hashSuffix
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
		if err := policybypass.RevokePolicyURL(ctx, db, organizationID, policyID, canonicalURL); err != nil {
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
		if err := policybypass.ReplacePolicyURLAudience(ctx, db, organizationID, policyID, canonicalURL, principals); err != nil {
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
		if grant.Effect != authz.PolicyEffectAllow {
			continue
		}
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

func (s *Service) revokeShadowMCPInventoryURLRequests(
	ctx context.Context,
	db riskrepo.DBTX,
	authCtx *contextvalues.AuthContext,
	projectID uuid.UUID,
	canonicalURL string,
) error {
	policies, err := s.shadowMCPInventoryProjectPolicies(ctx, db, projectID)
	if err != nil {
		return err
	}
	policiesByID := make(map[string]riskrepo.RiskPolicy, len(policies))
	for _, policy := range policies {
		policiesByID[policy.ID.String()] = policy
	}

	q := riskrepo.New(db)
	requests, err := q.ListRiskPolicyBypassRequests(ctx, riskrepo.ListRiskPolicyBypassRequestsParams{
		ProjectID:    projectID,
		RiskPolicyID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Status:       conv.ToPGText(shadowMCPInventoryBypassStatusApproved),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list approved shadow mcp inventory requests").LogError(ctx, s.logger)
	}

	for _, request := range requests {
		policy, ok := policiesByID[request.RiskPolicyID.String()]
		if !ok {
			continue
		}
		if conv.FromPGTextOrEmpty[string](request.TargetKind) != shadowMCPInventoryBypassTargetKind {
			continue
		}
		dimensions, err := shadowMCPInventoryBypassDimensions(request.TargetDimensions)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "parse approved shadow mcp inventory request dimensions").LogError(ctx, s.logger)
		}
		if dimensions[authz.SelectorKeyServerURL] != canonicalURL {
			continue
		}
		updated, err := q.UpdateRiskPolicyBypassRequestStatus(ctx, riskrepo.UpdateRiskPolicyBypassRequestStatusParams{
			Status:               shadowMCPInventoryBypassStatusRevoked,
			DecidedBy:            conv.ToPGText(authCtx.UserID),
			GrantedPrincipalUrns: []string{},
			ID:                   request.ID,
			ProjectID:            projectID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "revoke shadow mcp inventory request").LogError(ctx, s.logger)
		}

		if err := s.audit.LogRiskPolicyBypassRequestRevoke(ctx, db, audit.LogRiskPolicyBypassRequestEvent{
			OrganizationID:                    authCtx.ActiveOrganizationID,
			ProjectID:                         projectID,
			Actor:                             urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:                  authCtx.Email,
			ActorSlug:                         nil,
			RiskPolicyID:                      request.RiskPolicyID,
			RiskPolicyName:                    policy.Name,
			PolicyBypassRequestSnapshotBefore: shadowMCPInventoryBypassRequestAuditSnapshot(request, dimensions),
			PolicyBypassRequestSnapshotAfter:  shadowMCPInventoryBypassRequestAuditSnapshot(updated, dimensions),
			Metadata: &audit.RiskPolicyBypassRequestMetadata{
				RequestID:            updated.ID.String(),
				TargetKind:           conv.FromPGTextOrEmpty[string](updated.TargetKind),
				TargetKey:            conv.FromPGTextOrEmpty[string](updated.TargetKey),
				TargetDimensions:     maps.Clone(dimensions),
				RequesterUserID:      updated.RequesterUserID,
				GrantedPrincipalURNs: slices.Clone(updated.GrantedPrincipalUrns),
				PreviousStatus:       request.Status,
				CurrentStatus:        updated.Status,
			},
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log shadow mcp inventory request revocation").LogError(ctx, s.logger)
		}
	}
	return nil
}

func shadowMCPInventoryBypassRequestAuditSnapshot(request riskrepo.RiskPolicyBypassRequest, dimensions map[string]string) *audit.RiskPolicyBypassRequestSnapshot {
	return &audit.RiskPolicyBypassRequestSnapshot{
		ID:                   request.ID.String(),
		PolicyID:             request.RiskPolicyID.String(),
		TargetKind:           conv.FromPGText[string](request.TargetKind),
		TargetLabel:          conv.FromPGText[string](request.TargetLabel),
		TargetKey:            conv.FromPGText[string](request.TargetKey),
		TargetDimensions:     maps.Clone(dimensions),
		RequesterUserID:      request.RequesterUserID,
		RequesterEmail:       conv.FromPGText[string](request.RequesterEmail),
		Note:                 conv.FromPGText[string](request.Note),
		Status:               request.Status,
		DecidedBy:            conv.FromPGText[string](request.DecidedBy),
		GrantedPrincipalURNs: slices.Clone(request.GrantedPrincipalUrns),
		DecidedAt:            conv.PtrEmpty(conv.FromPGTimestamptz(request.DecidedAt)),
		CreatedAt:            conv.FromPGTimestamptz(request.CreatedAt),
		UpdatedAt:            conv.FromPGTimestamptz(request.UpdatedAt),
	}
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
		AllowedPolicyIds: rowState.AllowedPolicyIDs,
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
