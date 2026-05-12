package access

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	shadowMCPRequestStatusRequested = "requested"
	shadowMCPRequestStatusApproved  = "approved"
	shadowMCPRequestStatusDenied    = "denied"

	shadowMCPRuleAllowed = "allowed"
	shadowMCPRuleDenied  = "denied"
)

func (s *Service) ListShadowMCPApprovalRequests(ctx context.Context, payload *gen.ListShadowMCPApprovalRequestsPayload) (*gen.ListShadowMCPApprovalRequestsResult, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	status := conv.PtrValOr(payload.Status, "")
	projectID := conv.PtrValOr(payload.ProjectID, "")
	limit, offset, err := shadowMCPPagination(payload.Limit, payload.Offset)
	if err != nil {
		return nil, err
	}
	queries := repo.New(s.db)

	rows, err := queries.ListShadowMCPApprovalRequests(ctx, repo.ListShadowMCPApprovalRequestsParams{
		OrganizationID: ac.ActiveOrganizationID,
		Status:         status,
		ProjectID:      projectID,
		OffsetCount:    offset,
		LimitCount:     limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp approval requests").Log(ctx, s.logger)
	}

	total, err := queries.CountShadowMCPApprovalRequests(ctx, repo.CountShadowMCPApprovalRequestsParams{
		OrganizationID: ac.ActiveOrganizationID,
		Status:         status,
		ProjectID:      projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count shadow mcp approval requests").Log(ctx, s.logger)
	}

	requests := make([]*gen.ShadowMCPApprovalRequest, 0, len(rows))
	for _, row := range rows {
		requests = append(requests, buildShadowMCPApprovalRequest(row))
	}

	return &gen.ListShadowMCPApprovalRequestsResult{
		Requests: requests,
		Total:    int(total),
	}, nil
}

func (s *Service) CreateShadowMCPApprovalRequest(ctx context.Context, payload *gen.CreateShadowMCPApprovalRequestPayload) (*gen.ShadowMCPApprovalRequest, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	if ac.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "missing requester user").Log(ctx, s.logger)
	}
	claims, err := parseShadowMCPApprovalRequestToken(s.jwtSecret, payload.RequestToken)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid shadow mcp approval request token").Log(ctx, s.logger)
	}
	if claims.OrganizationID != ac.ActiveOrganizationID {
		return nil, oops.E(oops.CodeForbidden, nil, "shadow mcp approval request token is for a different organization").Log(ctx, s.logger)
	}
	if claims.RequesterUserID != "" && claims.RequesterUserID != ac.UserID {
		return nil, oops.E(oops.CodeForbidden, nil, "shadow mcp approval request token is for a different requester").Log(ctx, s.logger)
	}
	projectID, err := uuid.Parse(claims.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
	}
	if err := s.requireProjectInOrganization(ctx, ac.ActiveOrganizationID, projectID); err != nil {
		return nil, err
	}

	riskPolicyID, err := conv.PtrToNullUUID(claims.RiskPolicyID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid risk policy id").Log(ctx, s.logger)
	}
	riskResultID, err := conv.PtrToNullUUID(claims.RiskResultID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid risk result id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin shadow mcp approval request transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	row, err := queries.UpsertShadowMCPApprovalRequest(ctx, repo.UpsertShadowMCPApprovalRequestParams{
		OrganizationID:         ac.ActiveOrganizationID,
		ProjectID:              projectID,
		RequesterUserID:        conv.ToPGTextEmpty(ac.UserID),
		RequesterEmail:         conv.PtrToPGText(ac.Email),
		RequesterDisplayName:   conv.PtrToPGText(ac.Email),
		RiskPolicyID:           riskPolicyID,
		RiskResultID:           riskResultID,
		ObservedName:           conv.PtrToPGTextEmpty(claims.ObservedName),
		ObservedFullUrl:        conv.PtrToPGTextEmpty(claims.ObservedFullURL),
		ObservedUrlHost:        conv.PtrToPGTextEmpty(claims.ObservedURLHost),
		ObservedServerIdentity: conv.PtrToPGTextEmpty(claims.ObservedServerIdentity),
		RequestFingerprint:     conv.ToPGTextEmpty(shadowMCPApprovalRequestFingerprint(claims)),
		ToolName:               conv.PtrToPGTextEmpty(claims.ToolName),
		ToolCall:               conv.PtrToPGTextEmpty(claims.ToolCall),
		BlockReason:            conv.PtrToPGTextEmpty(claims.BlockReason),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert shadow mcp approval request").Log(ctx, s.logger)
	}
	requestView := buildShadowMCPApprovalRequest(row)
	if err := s.audit.LogShadowMCPApprovalRequestCreate(ctx, dbtx, audit.LogShadowMCPApprovalRequestEvent{
		OrganizationID:                ac.ActiveOrganizationID,
		ProjectID:                     projectID,
		Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:              ac.Email,
		ActorSlug:                     nil,
		ApprovalRequestURN:            urn.NewShadowMCPApprovalRequest(row.ID),
		DisplayName:                   shadowMCPApprovalRequestDisplayName(row),
		ApprovalRequestSnapshotBefore: nil,
		ApprovalRequestSnapshotAfter:  requestView,
		Metadata:                      nil,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp approval request create").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp approval request transaction").Log(ctx, s.logger)
	}

	return requestView, nil
}

func (s *Service) ApproveShadowMCPApprovalRequest(ctx context.Context, payload *gen.ApproveShadowMCPApprovalRequestPayload) (*gen.ShadowMCPApprovalDecisionResult, error) {
	ac, workosOrgID, err := s.requireOrgAdminWithWorkOS(ctx)
	if err != nil {
		return nil, err
	}
	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid request id").Log(ctx, s.logger)
	}
	matchValue, err := normalizeShadowMCPMatchValue(payload.MatchBreadth, payload.MatchValue)
	if err != nil {
		return nil, err
	}
	if err := validateShadowMCPAllowedRoleIDs(shadowMCPRuleAllowed, payload.RoleIds); err != nil {
		return nil, err
	}

	roleSlugs, roleIDBySlug, err := s.shadowMCPRoleMappings(ctx, workosOrgID, payload.RoleIds)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin shadow mcp approval transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	request, err := queries.GetShadowMCPApprovalRequest(ctx, repo.GetShadowMCPApprovalRequestParams{
		OrganizationID: ac.ActiveOrganizationID,
		ID:             requestID,
	})
	if err != nil {
		return nil, shadowMCPRepoErr(ctx, s, err, "get shadow mcp approval request")
	}
	if request.Status != shadowMCPRequestStatusRequested {
		return nil, oops.E(oops.CodeBadRequest, nil, "shadow mcp approval request has already been decided").Log(ctx, s.logger)
	}

	requestBefore := buildShadowMCPApprovalRequest(request)
	rule, createdRule, err := s.getOrCreateShadowMCPAccessRule(ctx, queries, ac, shadowMCPAccessRuleInput{
		Disposition:            shadowMCPRuleAllowed,
		MatchBreadth:           payload.MatchBreadth,
		MatchValue:             matchValue,
		DisplayName:            payload.DisplayName,
		ObservedFullURL:        coalesceString(payload.ObservedFullURL, conv.FromPGText[string](request.ObservedFullUrl)),
		ObservedURLHost:        coalesceString(payload.ObservedURLHost, conv.FromPGText[string](request.ObservedUrlHost)),
		ObservedServerIdentity: coalesceString(payload.ObservedServerIdentity, conv.FromPGText[string](request.ObservedServerIdentity)),
		SourceRequestID:        uuid.NullUUID{UUID: request.ID, Valid: true},
		Reason:                 payload.Reason,
	})
	if err != nil {
		return nil, err
	}
	ruleBefore := buildShadowMCPAccessRule(rule, nil)
	existingRoleSlugs, err := shadowMCPRoleSlugsForRule(ctx, queries, ac.ActiveOrganizationID, rule.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list existing shadow mcp access rule role grants").Log(ctx, s.logger)
	}
	mergedRoleSlugs := mergeRoleSlugs(existingRoleSlugs, roleSlugs)
	if err := syncShadowMCPAccessRuleRoleGrants(ctx, queries, ac.ActiveOrganizationID, rule.ID, mergedRoleSlugs); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync shadow mcp access rule role grants").Log(ctx, s.logger)
	}

	request, err = queries.DecideShadowMCPApprovalRequest(ctx, repo.DecideShadowMCPApprovalRequestParams{
		Status:         shadowMCPRequestStatusApproved,
		DecidedBy:      conv.ToPGTextEmpty(ac.UserID),
		DecisionNote:   conv.PtrToPGTextEmpty(payload.Reason),
		OrganizationID: ac.ActiveOrganizationID,
		ID:             request.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "approve shadow mcp approval request").Log(ctx, s.logger)
	}
	requestAfter := buildShadowMCPApprovalRequest(request)
	ruleAfter := buildShadowMCPAccessRule(rule, roleIDsForSlugs(mergedRoleSlugs, roleIDBySlug))
	if createdRule {
		if err := s.audit.LogShadowMCPAccessRuleCreate(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           ac.ActiveOrganizationID,
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:         ac.Email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(rule.ID),
			DisplayName:              rule.DisplayName,
			MatchValue:               rule.MatchValue,
			AccessRuleSnapshotBefore: nil,
			AccessRuleSnapshotAfter:  ruleAfter,
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: mergedRoleSlugs, Reason: payload.Reason},
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp access rule create").Log(ctx, s.logger)
		}
	} else {
		ruleBefore.RoleIds = roleIDsForSlugs(existingRoleSlugs, roleIDBySlug)
		if err := s.audit.LogShadowMCPAccessRuleUpdate(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           ac.ActiveOrganizationID,
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:         ac.Email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(rule.ID),
			DisplayName:              rule.DisplayName,
			MatchValue:               rule.MatchValue,
			AccessRuleSnapshotBefore: ruleBefore,
			AccessRuleSnapshotAfter:  ruleAfter,
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: mergedRoleSlugs, Reason: payload.Reason},
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp access rule update").Log(ctx, s.logger)
		}
	}
	if err := s.audit.LogShadowMCPApprovalRequestApprove(ctx, dbtx, audit.LogShadowMCPApprovalRequestEvent{
		OrganizationID:                ac.ActiveOrganizationID,
		ProjectID:                     request.ProjectID,
		Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:              ac.Email,
		ActorSlug:                     nil,
		ApprovalRequestURN:            urn.NewShadowMCPApprovalRequest(request.ID),
		DisplayName:                   shadowMCPApprovalRequestDisplayName(request),
		ApprovalRequestSnapshotBefore: requestBefore,
		ApprovalRequestSnapshotAfter:  requestAfter,
		Metadata:                      &audit.ShadowMCPAuditMetadata{RoleSlugs: mergedRoleSlugs, Reason: payload.Reason},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp approval request approve").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp approval transaction").Log(ctx, s.logger)
	}

	return &gen.ShadowMCPApprovalDecisionResult{
		Request: requestAfter,
		Rule:    ruleAfter,
	}, nil
}

func (s *Service) DenyShadowMCPApprovalRequest(ctx context.Context, payload *gen.DenyShadowMCPApprovalRequestPayload) (*gen.ShadowMCPApprovalDecisionResult, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid request id").Log(ctx, s.logger)
	}
	if payload.CreateDenyRule {
		if payload.MatchBreadth == nil || payload.MatchValue == nil || payload.DisplayName == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "match_breadth, match_value, and display_name are required when creating a deny rule").Log(ctx, s.logger)
		}
		if _, err := normalizeShadowMCPMatchValue(*payload.MatchBreadth, *payload.MatchValue); err != nil {
			return nil, err
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin shadow mcp denial transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	request, err := queries.GetShadowMCPApprovalRequest(ctx, repo.GetShadowMCPApprovalRequestParams{
		OrganizationID: ac.ActiveOrganizationID,
		ID:             requestID,
	})
	if err != nil {
		return nil, shadowMCPRepoErr(ctx, s, err, "get shadow mcp approval request")
	}
	if request.Status != shadowMCPRequestStatusRequested {
		return nil, oops.E(oops.CodeBadRequest, nil, "shadow mcp approval request has already been decided").Log(ctx, s.logger)
	}

	var rule *repo.ShadowMcpAccessRule
	ruleCreated := false
	if payload.CreateDenyRule {
		matchValue, err := normalizeShadowMCPMatchValue(*payload.MatchBreadth, *payload.MatchValue)
		if err != nil {
			return nil, err
		}
		created, createdRule, err := s.getOrCreateShadowMCPAccessRule(ctx, queries, ac, shadowMCPAccessRuleInput{
			Disposition:            shadowMCPRuleDenied,
			MatchBreadth:           *payload.MatchBreadth,
			MatchValue:             matchValue,
			DisplayName:            *payload.DisplayName,
			ObservedFullURL:        coalesceString(payload.ObservedFullURL, conv.FromPGText[string](request.ObservedFullUrl)),
			ObservedURLHost:        coalesceString(payload.ObservedURLHost, conv.FromPGText[string](request.ObservedUrlHost)),
			ObservedServerIdentity: coalesceString(payload.ObservedServerIdentity, conv.FromPGText[string](request.ObservedServerIdentity)),
			SourceRequestID:        uuid.NullUUID{UUID: request.ID, Valid: true},
			Reason:                 payload.Reason,
		})
		if err != nil {
			return nil, err
		}
		if err := syncShadowMCPAccessRuleRoleGrants(ctx, queries, ac.ActiveOrganizationID, created.ID, nil); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "clear deny rule role grants").Log(ctx, s.logger)
		}
		rule = &created
		ruleCreated = createdRule
	}

	requestBefore := buildShadowMCPApprovalRequest(request)
	request, err = queries.DecideShadowMCPApprovalRequest(ctx, repo.DecideShadowMCPApprovalRequestParams{
		Status:         shadowMCPRequestStatusDenied,
		DecidedBy:      conv.ToPGTextEmpty(ac.UserID),
		DecisionNote:   conv.PtrToPGTextEmpty(payload.Reason),
		OrganizationID: ac.ActiveOrganizationID,
		ID:             request.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "deny shadow mcp approval request").Log(ctx, s.logger)
	}
	requestAfter := buildShadowMCPApprovalRequest(request)
	if rule != nil && ruleCreated {
		ruleAfter := buildShadowMCPAccessRule(*rule, nil)
		if err := s.audit.LogShadowMCPAccessRuleCreate(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           ac.ActiveOrganizationID,
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:         ac.Email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(rule.ID),
			DisplayName:              rule.DisplayName,
			MatchValue:               rule.MatchValue,
			AccessRuleSnapshotBefore: nil,
			AccessRuleSnapshotAfter:  ruleAfter,
			Metadata:                 &audit.ShadowMCPAuditMetadata{Reason: payload.Reason},
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp access rule create").Log(ctx, s.logger)
		}
	}
	if err := s.audit.LogShadowMCPApprovalRequestDeny(ctx, dbtx, audit.LogShadowMCPApprovalRequestEvent{
		OrganizationID:                ac.ActiveOrganizationID,
		ProjectID:                     request.ProjectID,
		Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:              ac.Email,
		ActorSlug:                     nil,
		ApprovalRequestURN:            urn.NewShadowMCPApprovalRequest(request.ID),
		DisplayName:                   shadowMCPApprovalRequestDisplayName(request),
		ApprovalRequestSnapshotBefore: requestBefore,
		ApprovalRequestSnapshotAfter:  requestAfter,
		Metadata:                      &audit.ShadowMCPAuditMetadata{Reason: payload.Reason},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp approval request deny").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp denial transaction").Log(ctx, s.logger)
	}

	var ruleView *gen.ShadowMCPAccessRule
	if rule != nil {
		ruleView = buildShadowMCPAccessRule(*rule, nil)
	}
	return &gen.ShadowMCPApprovalDecisionResult{
		Request: requestAfter,
		Rule:    ruleView,
	}, nil
}

func (s *Service) ListShadowMCPAccessRules(ctx context.Context, payload *gen.ListShadowMCPAccessRulesPayload) (*gen.ListShadowMCPAccessRulesResult, error) {
	ac, workosOrgID, err := s.requireOrgReadWithWorkOS(ctx)
	if err != nil {
		return nil, err
	}

	disposition := conv.PtrValOr(payload.Disposition, "")
	limit, offset, err := shadowMCPPagination(payload.Limit, payload.Offset)
	if err != nil {
		return nil, err
	}
	queries := repo.New(s.db)
	rows, err := queries.ListShadowMCPAccessRules(ctx, repo.ListShadowMCPAccessRulesParams{
		OrganizationID: ac.ActiveOrganizationID,
		Disposition:    disposition,
		OffsetCount:    offset,
		LimitCount:     limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp access rules").Log(ctx, s.logger)
	}
	total, err := queries.CountShadowMCPAccessRules(ctx, repo.CountShadowMCPAccessRulesParams{
		OrganizationID: ac.ActiveOrganizationID,
		Disposition:    disposition,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count shadow mcp access rules").Log(ctx, s.logger)
	}

	roleIDsByRule, err := s.shadowMCPRoleIDsByRule(ctx, queries, ac.ActiveOrganizationID, workosOrgID, rows)
	if err != nil {
		return nil, err
	}
	rules := make([]*gen.ShadowMCPAccessRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, buildShadowMCPAccessRule(row, roleIDsByRule[row.ID.String()]))
	}

	return &gen.ListShadowMCPAccessRulesResult{
		Rules: rules,
		Total: int(total),
	}, nil
}

func (s *Service) CreateShadowMCPAccessRule(ctx context.Context, payload *gen.CreateShadowMCPAccessRulePayload) (*gen.ShadowMCPAccessRule, error) {
	ac, workosOrgID, err := s.requireOrgAdminWithWorkOS(ctx)
	if err != nil {
		return nil, err
	}
	matchValue, err := normalizeShadowMCPMatchValue(payload.MatchBreadth, payload.MatchValue)
	if err != nil {
		return nil, err
	}
	if err := validateShadowMCPDisposition(payload.Disposition); err != nil {
		return nil, err
	}
	if err := validateShadowMCPAllowedRoleIDs(payload.Disposition, payload.RoleIds); err != nil {
		return nil, err
	}
	roleSlugs, roleIDBySlug, err := s.shadowMCPRoleMappings(ctx, workosOrgID, payload.RoleIds)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin shadow mcp access rule transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	rule, err := queries.CreateShadowMCPAccessRule(ctx, repo.CreateShadowMCPAccessRuleParams{
		OrganizationID:         ac.ActiveOrganizationID,
		Disposition:            payload.Disposition,
		MatchBreadth:           payload.MatchBreadth,
		MatchValue:             matchValue,
		DisplayName:            payload.DisplayName,
		ObservedFullUrl:        conv.PtrToPGTextEmpty(payload.ObservedFullURL),
		ObservedUrlHost:        conv.PtrToPGTextEmpty(payload.ObservedURLHost),
		ObservedServerIdentity: conv.PtrToPGTextEmpty(payload.ObservedServerIdentity),
		SourceRequestID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CreatedBy:              conv.ToPGTextEmpty(ac.UserID),
		UpdatedBy:              conv.ToPGTextEmpty(ac.UserID),
		Reason:                 conv.PtrToPGTextEmpty(payload.Reason),
	})
	if err != nil {
		return nil, shadowMCPCreateRuleErr(ctx, s, err)
	}
	if err := syncShadowMCPAccessRuleRoleGrants(ctx, queries, ac.ActiveOrganizationID, rule.ID, roleSlugsForDisposition(payload.Disposition, roleSlugs)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync shadow mcp access rule role grants").Log(ctx, s.logger)
	}
	syncedSlugs := roleSlugsForDisposition(payload.Disposition, roleSlugs)
	ruleView := buildShadowMCPAccessRule(rule, roleIDsForSlugs(syncedSlugs, roleIDBySlug))
	if err := s.audit.LogShadowMCPAccessRuleCreate(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
		OrganizationID:           ac.ActiveOrganizationID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:         ac.Email,
		ActorSlug:                nil,
		AccessRuleURN:            urn.NewShadowMCPAccessRule(rule.ID),
		DisplayName:              rule.DisplayName,
		MatchValue:               rule.MatchValue,
		AccessRuleSnapshotBefore: nil,
		AccessRuleSnapshotAfter:  ruleView,
		Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: syncedSlugs, Reason: payload.Reason},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp access rule create").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp access rule transaction").Log(ctx, s.logger)
	}

	return ruleView, nil
}

func (s *Service) UpdateShadowMCPAccessRule(ctx context.Context, payload *gen.UpdateShadowMCPAccessRulePayload) (*gen.ShadowMCPAccessRule, error) {
	ac, workosOrgID, err := s.requireOrgAdminWithWorkOS(ctx)
	if err != nil {
		return nil, err
	}
	ruleID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid rule id").Log(ctx, s.logger)
	}
	matchValue, err := normalizeShadowMCPMatchValue(payload.MatchBreadth, payload.MatchValue)
	if err != nil {
		return nil, err
	}
	if err := validateShadowMCPDisposition(payload.Disposition); err != nil {
		return nil, err
	}
	if err := validateShadowMCPAllowedRoleIDs(payload.Disposition, payload.RoleIds); err != nil {
		return nil, err
	}
	roleSlugs, roleIDBySlug, err := s.shadowMCPRoleMappings(ctx, workosOrgID, payload.RoleIds)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin shadow mcp access rule update transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	existingRule, err := queries.GetShadowMCPAccessRule(ctx, repo.GetShadowMCPAccessRuleParams{
		OrganizationID: ac.ActiveOrganizationID,
		ID:             ruleID,
	})
	if err != nil {
		return nil, shadowMCPRepoErr(ctx, s, err, "get shadow mcp access rule")
	}
	existingRoleSlugs, err := shadowMCPRoleSlugsForRule(ctx, queries, ac.ActiveOrganizationID, ruleID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list existing shadow mcp access rule role grants").Log(ctx, s.logger)
	}
	rule, err := queries.UpdateShadowMCPAccessRule(ctx, repo.UpdateShadowMCPAccessRuleParams{
		Disposition:            payload.Disposition,
		MatchBreadth:           payload.MatchBreadth,
		MatchValue:             matchValue,
		DisplayName:            payload.DisplayName,
		ObservedFullUrl:        conv.PtrToPGTextEmpty(payload.ObservedFullURL),
		ObservedUrlHost:        conv.PtrToPGTextEmpty(payload.ObservedURLHost),
		ObservedServerIdentity: conv.PtrToPGTextEmpty(payload.ObservedServerIdentity),
		UpdatedBy:              conv.ToPGTextEmpty(ac.UserID),
		Reason:                 conv.PtrToPGTextEmpty(payload.Reason),
		OrganizationID:         ac.ActiveOrganizationID,
		ID:                     ruleID,
	})
	if err != nil {
		return nil, shadowMCPRepoErr(ctx, s, err, "update shadow mcp access rule")
	}
	syncedSlugs := roleSlugsForDisposition(payload.Disposition, roleSlugs)
	if err := syncShadowMCPAccessRuleRoleGrants(ctx, queries, ac.ActiveOrganizationID, rule.ID, syncedSlugs); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "sync shadow mcp access rule role grants").Log(ctx, s.logger)
	}
	ruleBefore := buildShadowMCPAccessRule(existingRule, roleIDsForSlugs(existingRoleSlugs, roleIDBySlug))
	ruleAfter := buildShadowMCPAccessRule(rule, roleIDsForSlugs(syncedSlugs, roleIDBySlug))
	if err := s.audit.LogShadowMCPAccessRuleUpdate(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
		OrganizationID:           ac.ActiveOrganizationID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:         ac.Email,
		ActorSlug:                nil,
		AccessRuleURN:            urn.NewShadowMCPAccessRule(rule.ID),
		DisplayName:              rule.DisplayName,
		MatchValue:               rule.MatchValue,
		AccessRuleSnapshotBefore: ruleBefore,
		AccessRuleSnapshotAfter:  ruleAfter,
		Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: syncedSlugs, Reason: payload.Reason},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp access rule update").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp access rule update transaction").Log(ctx, s.logger)
	}

	return ruleAfter, nil
}

func (s *Service) DeleteShadowMCPAccessRule(ctx context.Context, payload *gen.DeleteShadowMCPAccessRulePayload) error {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return err
	}
	ruleID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid rule id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin shadow mcp access rule delete transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	rule, err := queries.DeleteShadowMCPAccessRule(ctx, repo.DeleteShadowMCPAccessRuleParams{
		UpdatedBy:      conv.ToPGTextEmpty(ac.UserID),
		OrganizationID: ac.ActiveOrganizationID,
		ID:             ruleID,
	})
	if err != nil {
		return shadowMCPRepoErr(ctx, s, err, "delete shadow mcp access rule")
	}
	if err := syncShadowMCPAccessRuleRoleGrants(ctx, queries, ac.ActiveOrganizationID, ruleID, nil); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete shadow mcp access rule role grants").Log(ctx, s.logger)
	}
	if err := s.audit.LogShadowMCPAccessRuleDelete(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
		OrganizationID:           ac.ActiveOrganizationID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:         ac.Email,
		ActorSlug:                nil,
		AccessRuleURN:            urn.NewShadowMCPAccessRule(rule.ID),
		DisplayName:              rule.DisplayName,
		MatchValue:               rule.MatchValue,
		AccessRuleSnapshotBefore: buildShadowMCPAccessRule(rule, nil),
		AccessRuleSnapshotAfter:  nil,
		Metadata:                 nil,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log shadow mcp access rule delete").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit shadow mcp access rule delete transaction").Log(ctx, s.logger)
	}

	return nil
}

type shadowMCPAccessRuleInput struct {
	Disposition            string
	MatchBreadth           string
	MatchValue             string
	DisplayName            string
	ObservedFullURL        *string
	ObservedURLHost        *string
	ObservedServerIdentity *string
	SourceRequestID        uuid.NullUUID
	Reason                 *string
}

func (s *Service) getOrCreateShadowMCPAccessRule(ctx context.Context, queries *repo.Queries, ac *contextvalues.AuthContext, input shadowMCPAccessRuleInput) (repo.ShadowMcpAccessRule, bool, error) {
	existing, err := queries.GetShadowMCPAccessRuleByMatch(ctx, repo.GetShadowMCPAccessRuleByMatchParams{
		OrganizationID: ac.ActiveOrganizationID,
		MatchBreadth:   input.MatchBreadth,
		MatchValue:     input.MatchValue,
	})
	switch {
	case err == nil:
		if existing.Disposition != input.Disposition {
			return repo.ShadowMcpAccessRule{}, false, oops.E(oops.CodeConflict, nil, "shadow mcp access rule already exists with a different disposition").Log(ctx, s.logger)
		}
		return existing, false, nil
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return repo.ShadowMcpAccessRule{}, false, oops.E(oops.CodeUnexpected, err, "get shadow mcp access rule by match").Log(ctx, s.logger)
	}

	rule, err := queries.CreateShadowMCPAccessRule(ctx, repo.CreateShadowMCPAccessRuleParams{
		OrganizationID:         ac.ActiveOrganizationID,
		Disposition:            input.Disposition,
		MatchBreadth:           input.MatchBreadth,
		MatchValue:             input.MatchValue,
		DisplayName:            input.DisplayName,
		ObservedFullUrl:        conv.PtrToPGTextEmpty(input.ObservedFullURL),
		ObservedUrlHost:        conv.PtrToPGTextEmpty(input.ObservedURLHost),
		ObservedServerIdentity: conv.PtrToPGTextEmpty(input.ObservedServerIdentity),
		SourceRequestID:        input.SourceRequestID,
		CreatedBy:              conv.ToPGTextEmpty(ac.UserID),
		UpdatedBy:              conv.ToPGTextEmpty(ac.UserID),
		Reason:                 conv.PtrToPGTextEmpty(input.Reason),
	})
	if err != nil {
		return repo.ShadowMcpAccessRule{}, false, shadowMCPCreateRuleErr(ctx, s, err)
	}
	return rule, true, nil
}

func (s *Service) requireOrgRead(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	return ac, nil
}

func (s *Service) requireOrgAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").Log(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	return ac, nil
}

func (s *Service) requireOrgReadWithWorkOS(ctx context.Context) (*contextvalues.AuthContext, string, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, "", err
	}
	return ac, workosOrgID, nil
}

func (s *Service) requireOrgAdminWithWorkOS(ctx context.Context) (*contextvalues.AuthContext, string, error) {
	ac, workosOrgID, err := s.roleOrgContext(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, "", err
	}
	return ac, workosOrgID, nil
}

func (s *Service) requireProjectInOrganization(ctx context.Context, organizationID string, projectID uuid.UUID) error {
	project, err := projectsrepo.New(s.db).GetProjectByID(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, nil, "project not found").Log(ctx, s.logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "get project").Log(ctx, s.logger)
	case project.OrganizationID != organizationID:
		return oops.E(oops.CodeNotFound, nil, "project not found").Log(ctx, s.logger)
	default:
		return nil
	}
}

func (s *Service) shadowMCPRoleMappings(ctx context.Context, workosOrgID string, roleIDs []string) ([]string, map[string]string, error) {
	roles, err := s.roles.ListRoles(ctx, workosOrgID)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "list roles from workos").Log(ctx, s.logger)
	}
	slugByID := make(map[string]string, len(roles))
	idBySlug := make(map[string]string, len(roles))
	for _, role := range roles {
		slugByID[role.ID] = role.Slug
		idBySlug[role.Slug] = role.ID
	}

	roleSlugs := make([]string, 0, len(roleIDs))
	seen := make(map[string]struct{}, len(roleIDs))
	for _, roleID := range roleIDs {
		slug, ok := slugByID[roleID]
		if !ok {
			return nil, nil, oops.E(oops.CodeNotFound, nil, "role not found").Log(ctx, s.logger)
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		roleSlugs = append(roleSlugs, slug)
	}
	return roleSlugs, idBySlug, nil
}

func (s *Service) shadowMCPRoleIDsByRule(ctx context.Context, queries *repo.Queries, organizationID string, workosOrgID string, rules []repo.ShadowMcpAccessRule) (map[string][]string, error) {
	out := make(map[string][]string, len(rules))
	if len(rules) == 0 {
		return out, nil
	}

	roleSlugs := make(map[string]struct{})
	ruleIDs := make([]string, 0, len(rules))
	for _, rule := range rules {
		ruleIDs = append(ruleIDs, rule.ID.String())
	}
	rows, err := queries.ListShadowMCPAccessRuleRoleGrants(ctx, repo.ListShadowMCPAccessRuleRoleGrantsParams{
		OrganizationID: organizationID,
		RuleIds:        ruleIDs,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp access rule role grants").Log(ctx, s.logger)
	}
	for _, row := range rows {
		if row.PrincipalUrn.Type != urn.PrincipalTypeRole {
			continue
		}
		roleSlugs[row.PrincipalUrn.ID] = struct{}{}
	}

	_, idBySlug, err := s.shadowMCPRoleMappings(ctx, workosOrgID, nil)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, ok := roleSlugs[row.PrincipalUrn.ID]; !ok {
			continue
		}
		roleID, ok := idBySlug[row.PrincipalUrn.ID]
		if !ok {
			continue
		}
		out[row.RuleID] = append(out[row.RuleID], roleID)
	}
	return out, nil
}

func shadowMCPRoleSlugsForRule(ctx context.Context, queries *repo.Queries, organizationID string, ruleID uuid.UUID) ([]string, error) {
	rows, err := queries.ListShadowMCPAccessRuleRoleGrants(ctx, repo.ListShadowMCPAccessRuleRoleGrantsParams{
		OrganizationID: organizationID,
		RuleIds:        []string{ruleID.String()},
	})
	if err != nil {
		return nil, fmt.Errorf("list shadow mcp access rule role grants: %w", err)
	}
	roleSlugs := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.PrincipalUrn.Type != urn.PrincipalTypeRole {
			continue
		}
		roleSlugs = append(roleSlugs, row.PrincipalUrn.ID)
	}
	return roleSlugs, nil
}

func syncShadowMCPAccessRuleRoleGrants(ctx context.Context, queries *repo.Queries, organizationID string, ruleID uuid.UUID, roleSlugs []string) error {
	if _, err := queries.DeleteShadowMCPAccessRuleRoleGrants(ctx, repo.DeleteShadowMCPAccessRuleRoleGrantsParams{
		OrganizationID: organizationID,
		RuleID:         ruleID.String(),
	}); err != nil {
		return fmt.Errorf("delete existing role grants: %w", err)
	}

	selectors, err := authz.NewSelector(authz.ScopeShadowMCPConnect, ruleID.String()).MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal selector: %w", err)
	}
	for _, roleSlug := range roleSlugs {
		if _, err := queries.UpsertPrincipalGrant(ctx, repo.UpsertPrincipalGrantParams{
			OrganizationID: organizationID,
			PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug),
			Scope:          string(authz.ScopeShadowMCPConnect),
			Selectors:      selectors,
		}); err != nil {
			return fmt.Errorf("upsert role grant for %q: %w", roleSlug, err)
		}
	}

	return nil
}

func buildShadowMCPApprovalRequest(row repo.ShadowMcpApprovalRequest) *gen.ShadowMCPApprovalRequest {
	return &gen.ShadowMCPApprovalRequest{
		ID:                     row.ID.String(),
		OrganizationID:         row.OrganizationID,
		ProjectID:              row.ProjectID.String(),
		RequesterUserID:        conv.FromPGText[string](row.RequesterUserID),
		RequesterEmail:         conv.FromPGText[string](row.RequesterEmail),
		RequesterDisplayName:   conv.FromPGText[string](row.RequesterDisplayName),
		Status:                 row.Status,
		RiskPolicyID:           conv.FromNullableUUID(row.RiskPolicyID),
		RiskResultID:           conv.FromNullableUUID(row.RiskResultID),
		ObservedName:           conv.FromPGText[string](row.ObservedName),
		ObservedFullURL:        conv.FromPGText[string](row.ObservedFullUrl),
		ObservedURLHost:        conv.FromPGText[string](row.ObservedUrlHost),
		ObservedServerIdentity: conv.FromPGText[string](row.ObservedServerIdentity),
		ToolName:               conv.FromPGText[string](row.ToolName),
		ToolCall:               conv.FromPGText[string](row.ToolCall),
		BlockReason:            conv.FromPGText[string](row.BlockReason),
		BlockedCount:           int(row.BlockedCount),
		FirstBlockedAt:         formatPGTimestamp(row.FirstBlockedAt),
		LastBlockedAt:          formatPGTimestamp(row.LastBlockedAt),
		RequestedAt:            formatPGTimestampValue(row.RequestedAt),
		DecidedAt:              formatPGTimestamp(row.DecidedAt),
		DecidedBy:              conv.FromPGText[string](row.DecidedBy),
		DecisionNote:           conv.FromPGText[string](row.DecisionNote),
		CreatedAt:              formatPGTimestampValue(row.CreatedAt),
		UpdatedAt:              formatPGTimestampValue(row.UpdatedAt),
	}
}

func shadowMCPApprovalRequestDisplayName(row repo.ShadowMcpApprovalRequest) string {
	if value := conv.FromPGText[string](row.ObservedName); value != nil {
		return *value
	}
	if value := conv.FromPGText[string](row.ObservedFullUrl); value != nil {
		return *value
	}
	if value := conv.FromPGText[string](row.ObservedUrlHost); value != nil {
		return *value
	}
	if value := conv.FromPGText[string](row.ObservedServerIdentity); value != nil {
		return *value
	}
	return row.ID.String()
}

func buildShadowMCPAccessRule(row repo.ShadowMcpAccessRule, roleIDs []string) *gen.ShadowMCPAccessRule {
	return &gen.ShadowMCPAccessRule{
		ID:                     row.ID.String(),
		OrganizationID:         row.OrganizationID,
		Disposition:            row.Disposition,
		MatchBreadth:           row.MatchBreadth,
		MatchValue:             row.MatchValue,
		DisplayName:            row.DisplayName,
		ObservedFullURL:        conv.FromPGText[string](row.ObservedFullUrl),
		ObservedURLHost:        conv.FromPGText[string](row.ObservedUrlHost),
		ObservedServerIdentity: conv.FromPGText[string](row.ObservedServerIdentity),
		SourceRequestID:        conv.FromNullableUUID(row.SourceRequestID),
		CreatedBy:              conv.FromPGText[string](row.CreatedBy),
		UpdatedBy:              conv.FromPGText[string](row.UpdatedBy),
		Reason:                 conv.FromPGText[string](row.Reason),
		RoleIds:                roleIDs,
		CreatedAt:              formatPGTimestampValue(row.CreatedAt),
		UpdatedAt:              formatPGTimestampValue(row.UpdatedAt),
	}
}

func validateShadowMCPEvidence(fullURL, urlHost, serverIdentity *string) error {
	if conv.PtrValOr(fullURL, "") == "" && conv.PtrValOr(urlHost, "") == "" && conv.PtrValOr(serverIdentity, "") == "" {
		return oops.E(oops.CodeBadRequest, nil, "at least one observed server identity is required")
	}
	return nil
}

func validateShadowMCPMatch(matchBreadth string, matchValue string) error {
	if strings.TrimSpace(matchValue) == "" {
		return oops.E(oops.CodeBadRequest, nil, "match_value is required")
	}
	switch matchBreadth {
	case "full_url", "url_host", "server_identity":
		return nil
	default:
		return oops.E(oops.CodeBadRequest, nil, "invalid match_breadth")
	}
}

func normalizeShadowMCPMatchValue(matchBreadth string, matchValue string) (string, error) {
	if err := validateShadowMCPMatch(matchBreadth, matchValue); err != nil {
		return "", err
	}
	value := strings.TrimSpace(matchValue)
	switch matchBreadth {
	case "full_url":
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return "", oops.E(oops.CodeBadRequest, err, "match_value must be a full URL")
		}
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = normalizeShadowMCPHost(u.Host)
		u.Fragment = ""
		return u.String(), nil
	case "url_host":
		if strings.Contains(value, "://") {
			u, err := url.Parse(value)
			if err != nil || u.Host == "" {
				return "", oops.E(oops.CodeBadRequest, err, "match_value must include a URL host")
			}
			value = u.Host
		}
		return normalizeShadowMCPHost(value), nil
	case "server_identity":
		return strings.ToLower(value), nil
	default:
		return "", oops.E(oops.CodeBadRequest, nil, "invalid match_breadth")
	}
}

func normalizeShadowMCPHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	if port == "80" || port == "443" {
		return name
	}
	return net.JoinHostPort(name, port)
}

func validateShadowMCPDisposition(disposition string) error {
	switch disposition {
	case shadowMCPRuleAllowed, shadowMCPRuleDenied:
		return nil
	default:
		return oops.E(oops.CodeBadRequest, nil, "invalid disposition")
	}
}

func validateShadowMCPAllowedRoleIDs(disposition string, roleIDs []string) error {
	if disposition == shadowMCPRuleAllowed && len(roleIDs) == 0 {
		return oops.E(oops.CodeBadRequest, nil, "role_ids is required for allowed shadow mcp access rules")
	}
	return nil
}

func shadowMCPRepoErr(ctx context.Context, s *Service, err error, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, nil, "%s", message).Log(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "%s", message).Log(ctx, s.logger)
}

func shadowMCPCreateRuleErr(ctx context.Context, s *Service, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return oops.E(oops.CodeConflict, nil, "shadow mcp access rule already exists").Log(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "create shadow mcp access rule").Log(ctx, s.logger)
}

func formatPGTimestamp(ts pgtype.Timestamptz) *string {
	if !ts.Valid {
		return nil
	}
	value := ts.Time.UTC().Format(time.RFC3339)
	return &value
}

func formatPGTimestampValue(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return time.Time{}.UTC().Format(time.RFC3339)
	}
	return ts.Time.UTC().Format(time.RFC3339)
}

func shadowMCPPagination(limit, offset int) (int32, int32, error) {
	if limit < 0 {
		return 0, 0, oops.E(oops.CodeBadRequest, nil, "limit must be greater than or equal to 0")
	}
	if offset < 0 {
		return 0, 0, oops.E(oops.CodeBadRequest, nil, "offset must be greater than or equal to 0")
	}
	return conv.SafeInt32(limit), conv.SafeInt32(offset), nil
}

func coalesceString(primary *string, fallback *string) *string {
	if primary != nil {
		return primary
	}
	return fallback
}

func roleSlugsForDisposition(disposition string, roleSlugs []string) []string {
	if disposition != shadowMCPRuleAllowed {
		return nil
	}
	return roleSlugs
}

func roleIDsForSlugs(roleSlugs []string, roleIDBySlug map[string]string) []string {
	roleIDs := make([]string, 0, len(roleSlugs))
	for _, slug := range roleSlugs {
		if id, ok := roleIDBySlug[slug]; ok {
			roleIDs = append(roleIDs, id)
		}
	}
	return roleIDs
}

func mergeRoleSlugs(existingRoleSlugs []string, newRoleSlugs []string) []string {
	merged := make([]string, 0, len(existingRoleSlugs)+len(newRoleSlugs))
	seen := make(map[string]struct{}, len(existingRoleSlugs)+len(newRoleSlugs))
	for _, roleSlug := range existingRoleSlugs {
		if _, ok := seen[roleSlug]; ok {
			continue
		}
		seen[roleSlug] = struct{}{}
		merged = append(merged, roleSlug)
	}
	for _, roleSlug := range newRoleSlugs {
		if _, ok := seen[roleSlug]; ok {
			continue
		}
		seen[roleSlug] = struct{}{}
		merged = append(merged, roleSlug)
	}
	return merged
}
