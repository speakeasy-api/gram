package access

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
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
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	shadowMCPRequestStatusRequested = "requested"
	shadowMCPRequestStatusApproved  = "approved"
	shadowMCPRequestStatusDenied    = "denied"

	shadowMCPRuleAllowed = "allowed"
	shadowMCPRuleDenied  = "denied"

	shadowMCPAccessScopeOrganization = "organization"
	shadowMCPAccessScopeProject      = "project"

	shadowMCPMaxPageLimit = 1000
)

func (s *Service) ListShadowMCPApprovalRequests(ctx context.Context, payload *gen.ListShadowMCPApprovalRequestsPayload) (*gen.ListShadowMCPApprovalRequestsResult, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	status := conv.PtrValOr(payload.Status, "")
	projectID := conv.PtrValOr(payload.ProjectID, "")
	limit, err := shadowMCPLimit(payload.Limit)
	if err != nil {
		return nil, err
	}
	cursorRequestedAt, cursorID, err := decodeShadowMCPCursorParam(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
	}
	queries := repo.New(s.db)

	rows, err := queries.ListShadowMCPApprovalRequests(ctx, repo.ListShadowMCPApprovalRequestsParams{
		OrganizationID:    ac.ActiveOrganizationID,
		Status:            status,
		ProjectID:         projectID,
		CursorRequestedAt: cursorRequestedAt,
		CursorID:          cursorID,
		LimitCount:        limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp approval requests").Log(ctx, s.logger)
	}

	var nextCursor *string
	pageSize := int(limit)
	if len(rows) > pageSize {
		cursor := encodeShadowMCPCursor(rows[pageSize-1].RequestedAt, rows[pageSize-1].ID)
		nextCursor = &cursor
		rows = rows[:pageSize]
	}

	requests := make([]*gen.ShadowMCPApprovalRequest, 0, len(rows))
	for _, row := range rows {
		requests = append(requests, buildShadowMCPApprovalRequest(row))
	}

	return &gen.ListShadowMCPApprovalRequestsResult{
		Requests:   requests,
		NextCursor: nextCursor,
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
	ac, err := s.requireOrgAdmin(ctx)
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
	ruleProjectID, err := s.shadowMCPRuleProjectID(ctx, ac.ActiveOrganizationID, payload.AccessScope, nil, request.ProjectID)
	if err != nil {
		return nil, err
	}

	requestBefore := buildShadowMCPApprovalRequest(request)
	rule, createdRule, err := s.getOrCreateShadowMCPAccessRule(ctx, queries, ac, shadowMCPAccessRuleInput{
		ProjectID:              ruleProjectID,
		AccessScope:            payload.AccessScope,
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
	ruleBefore := buildShadowMCPAccessRule(rule)

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
	ruleAfter := buildShadowMCPAccessRule(rule)
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
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp access rule create").Log(ctx, s.logger)
		}
	} else {
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
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
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
		Metadata:                      &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
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
			ProjectID:              uuid.NullUUID{UUID: request.ProjectID, Valid: true},
			AccessScope:            shadowMCPAccessScopeProject,
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
		ruleAfter := buildShadowMCPAccessRule(*rule)
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
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
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
		Metadata:                      &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp approval request deny").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp denial transaction").Log(ctx, s.logger)
	}

	var ruleView *gen.ShadowMCPAccessRule
	if rule != nil {
		ruleView = buildShadowMCPAccessRule(*rule)
	}
	return &gen.ShadowMCPApprovalDecisionResult{
		Request: requestAfter,
		Rule:    ruleView,
	}, nil
}

func (s *Service) ListShadowMCPAccessRules(ctx context.Context, payload *gen.ListShadowMCPAccessRulesPayload) (*gen.ListShadowMCPAccessRulesResult, error) {
	ac, err := s.requireOrgRead(ctx)
	if err != nil {
		return nil, err
	}

	disposition := conv.PtrValOr(payload.Disposition, "")
	accessScope := conv.PtrValOr(payload.AccessScope, "")
	projectID := conv.PtrValOr(payload.ProjectID, "")
	limit, err := shadowMCPLimit(payload.Limit)
	if err != nil {
		return nil, err
	}
	cursorCreatedAt, cursorID, err := decodeShadowMCPCursorParam(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
	}
	queries := repo.New(s.db)
	rows, err := queries.ListShadowMCPAccessRules(ctx, repo.ListShadowMCPAccessRulesParams{
		OrganizationID:  ac.ActiveOrganizationID,
		Disposition:     disposition,
		AccessScope:     accessScope,
		ProjectID:       projectID,
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		LimitCount:      limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp access rules").Log(ctx, s.logger)
	}

	var nextCursor *string
	pageSize := int(limit)
	if len(rows) > pageSize {
		cursor := encodeShadowMCPCursor(rows[pageSize-1].CreatedAt, rows[pageSize-1].ID)
		nextCursor = &cursor
		rows = rows[:pageSize]
	}

	rules := make([]*gen.ShadowMCPAccessRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, buildShadowMCPAccessRule(row))
	}

	return &gen.ListShadowMCPAccessRulesResult{
		Rules:      rules,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) CreateShadowMCPAccessRule(ctx context.Context, payload *gen.CreateShadowMCPAccessRulePayload) (*gen.ShadowMCPAccessRule, error) {
	ac, err := s.requireOrgAdmin(ctx)
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
	ruleProjectID, err := s.shadowMCPRuleProjectID(ctx, ac.ActiveOrganizationID, payload.AccessScope, payload.ProjectID, uuid.Nil)
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
		ProjectID:              ruleProjectID,
		AccessScope:            payload.AccessScope,
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
	ruleView := buildShadowMCPAccessRule(rule)
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
		Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log shadow mcp access rule create").Log(ctx, s.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit shadow mcp access rule transaction").Log(ctx, s.logger)
	}

	return ruleView, nil
}

func (s *Service) UpdateShadowMCPAccessRule(ctx context.Context, payload *gen.UpdateShadowMCPAccessRulePayload) (*gen.ShadowMCPAccessRule, error) {
	ac, err := s.requireOrgAdmin(ctx)
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
	ruleProjectID, err := s.shadowMCPRuleProjectID(ctx, ac.ActiveOrganizationID, payload.AccessScope, payload.ProjectID, uuid.Nil)
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
	rule, err := queries.UpdateShadowMCPAccessRule(ctx, repo.UpdateShadowMCPAccessRuleParams{
		Disposition:            payload.Disposition,
		ProjectID:              ruleProjectID,
		AccessScope:            payload.AccessScope,
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
		return nil, shadowMCPWriteRuleErr(ctx, s, err, "update shadow mcp access rule")
	}
	ruleBefore := buildShadowMCPAccessRule(existingRule)
	ruleAfter := buildShadowMCPAccessRule(rule)
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
		Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
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
	if err := s.audit.LogShadowMCPAccessRuleDelete(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
		OrganizationID:           ac.ActiveOrganizationID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:         ac.Email,
		ActorSlug:                nil,
		AccessRuleURN:            urn.NewShadowMCPAccessRule(rule.ID),
		DisplayName:              rule.DisplayName,
		MatchValue:               rule.MatchValue,
		AccessRuleSnapshotBefore: buildShadowMCPAccessRule(rule),
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
	ProjectID              uuid.NullUUID
	AccessScope            string
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
		AccessScope:    input.AccessScope,
		ProjectID:      input.ProjectID,
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
		ProjectID:              input.ProjectID,
		AccessScope:            input.AccessScope,
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

func buildShadowMCPAccessRule(row repo.ShadowMcpAccessRule) *gen.ShadowMCPAccessRule {
	return &gen.ShadowMCPAccessRule{
		ID:                     row.ID.String(),
		OrganizationID:         row.OrganizationID,
		ProjectID:              conv.FromNullableUUID(row.ProjectID),
		AccessScope:            row.AccessScope,
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

func normalizeShadowMCPMatchValue(matchBreadth string, matchValue string) (string, error) {
	value, err := shadowmcp.NormalizeMatchValue(matchBreadth, matchValue)
	if err != nil {
		return "", oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}
	return value, nil
}

func validateShadowMCPDisposition(disposition string) error {
	switch disposition {
	case shadowMCPRuleAllowed, shadowMCPRuleDenied:
		return nil
	default:
		return oops.E(oops.CodeBadRequest, nil, "invalid disposition")
	}
}

func (s *Service) shadowMCPRuleProjectID(ctx context.Context, organizationID string, accessScope string, projectID *string, fallbackProjectID uuid.UUID) (uuid.NullUUID, error) {
	switch accessScope {
	case shadowMCPAccessScopeOrganization:
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	case shadowMCPAccessScopeProject:
		id := fallbackProjectID
		if projectID != nil && *projectID != "" {
			parsed, err := uuid.Parse(*projectID)
			if err != nil {
				return uuid.NullUUID{}, oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
			}
			id = parsed
		}
		if id == uuid.Nil {
			return uuid.NullUUID{}, oops.E(oops.CodeBadRequest, nil, "project_id is required for project-scoped shadow mcp access rules").Log(ctx, s.logger)
		}
		if err := s.requireProjectInOrganization(ctx, organizationID, id); err != nil {
			return uuid.NullUUID{}, err
		}
		return uuid.NullUUID{UUID: id, Valid: true}, nil
	default:
		return uuid.NullUUID{}, oops.E(oops.CodeBadRequest, nil, "invalid access_scope").Log(ctx, s.logger)
	}
}

func shadowMCPRepoErr(ctx context.Context, s *Service, err error, message string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeNotFound, nil, "%s", message).Log(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "%s", message).Log(ctx, s.logger)
}

func shadowMCPCreateRuleErr(ctx context.Context, s *Service, err error) error {
	return shadowMCPWriteRuleErr(ctx, s, err, "create shadow mcp access rule")
}

func shadowMCPWriteRuleErr(ctx context.Context, s *Service, err error, message string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return oops.E(oops.CodeConflict, nil, "shadow mcp access rule already exists").Log(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "%s", message).Log(ctx, s.logger)
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

func shadowMCPLimit(limit int) (int32, error) {
	if limit < 1 {
		return 0, oops.E(oops.CodeBadRequest, nil, "limit must be greater than or equal to 1")
	}
	if limit > shadowMCPMaxPageLimit {
		return 0, oops.E(oops.CodeBadRequest, nil, "limit must be less than or equal to 1000")
	}
	return conv.SafeInt32(limit), nil
}

func encodeShadowMCPCursor(ts pgtype.Timestamptz, id uuid.UUID) string {
	payload := fmt.Sprintf("%d:%s", ts.Time.UTC().UnixNano(), id.String())
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeShadowMCPCursorParam(cursor *string) (pgtype.Timestamptz, uuid.NullUUID, error) {
	if cursor == nil || *cursor == "" {
		return pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return pgtype.Timestamptz{}, uuid.NullUUID{}, fmt.Errorf("decode cursor: %w", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return pgtype.Timestamptz{}, uuid.NullUUID{}, fmt.Errorf("invalid cursor format")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return pgtype.Timestamptz{}, uuid.NullUUID{}, fmt.Errorf("parse cursor timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return pgtype.Timestamptz{}, uuid.NullUUID{}, fmt.Errorf("parse cursor id: %w", err)
	}

	return pgtype.Timestamptz{Time: time.Unix(0, nanos).UTC(), InfinityModifier: pgtype.Finite, Valid: true}, uuid.NullUUID{UUID: id, Valid: true}, nil
}

func coalesceString(primary *string, fallback *string) *string {
	if primary != nil {
		return primary
	}
	return fallback
}
