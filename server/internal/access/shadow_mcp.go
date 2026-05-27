package access

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/attr"
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
	shadowMCPResourceType = "shadow_mcp"

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
	cursor, err := decodeShadowMCPCursorParam(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
	}

	result, err := s.accessStore.ListRequests(ctx, accesscontrol.RequestFilters{
		OrganizationID: ac.ActiveOrganizationID,
		ResourceType:   shadowMCPResourceType,
		Status:         status,
		ProjectID:      projectID,
		Cursor:         cursor,
		Limit:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp approval requests").Log(ctx, s.logger)
	}

	var nextCursor *string
	if result.NextCursor != "" {
		cursor := encodeShadowMCPCursor(result.NextCursor)
		nextCursor = &cursor
	}

	requests := make([]*gen.ShadowMCPApprovalRequest, 0, len(result.Requests))
	for _, row := range result.Requests {
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

	if _, err := conv.PtrToNullUUID(claims.RiskPolicyID); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid risk policy id").Log(ctx, s.logger)
	}
	if _, err := conv.PtrToNullUUID(claims.RiskResultID); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid risk result id").Log(ctx, s.logger)
	}

	summary := shadowMCPSummaryFromClaims(claims)
	now := time.Now().UTC()
	request, wasCreated, err := s.accessStore.UpsertRequest(ctx, accesscontrol.AccessApprovalRequest{
		ID:                   "",
		OrganizationID:       ac.ActiveOrganizationID,
		ProjectID:            projectID.String(),
		ResourceType:         shadowMCPResourceType,
		Status:               shadowMCPRequestStatusRequested,
		RequesterUserID:      ac.UserID,
		RequesterEmail:       conv.PtrValOr(ac.Email, ""),
		RequesterDisplayName: conv.PtrValOr(ac.Email, ""),
		RequestFingerprint:   shadowMCPApprovalRequestFingerprint(claims),
		DisplayName:          shadowMCPSummaryDisplayName(summary, ""),
		ObservedSummary:      accessControlSummaryFromShadowMCPSummary(summary),
		BlockedCount:         1,
		FirstBlockedAt:       now,
		LastBlockedAt:        now,
		RequestedAt:          now,
		DecidedAt:            nil,
		DecidedBy:            "",
		DecisionNote:         "",
		SourceRuleIDs:        nil,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert shadow mcp approval request").Log(ctx, s.logger)
	}
	requestView := buildShadowMCPApprovalRequest(request)
	if wasCreated {
		requestUUID, err := uuid.Parse(request.ID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse shadow mcp approval request id").Log(ctx, s.logger)
		}
		s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp approval request create", func(dbtx pgx.Tx) error {
			return s.audit.LogShadowMCPApprovalRequestCreate(ctx, dbtx, audit.LogShadowMCPApprovalRequestEvent{
				OrganizationID:                ac.ActiveOrganizationID,
				ProjectID:                     projectID,
				Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
				ActorDisplayName:              ac.Email,
				ActorSlug:                     nil,
				ApprovalRequestURN:            urn.NewShadowMCPApprovalRequest(requestUUID),
				DisplayName:                   shadowMCPApprovalRequestDisplayName(request),
				ApprovalRequestSnapshotBefore: nil,
				ApprovalRequestSnapshotAfter:  requestView,
				Metadata:                      nil,
			})
		})
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

	request, err := s.accessStore.GetRequest(ctx, ac.ActiveOrganizationID, shadowMCPResourceType, payload.ID)
	if err != nil {
		return nil, shadowMCPStoreErr(ctx, s, err, "get shadow mcp approval request")
	}
	if request.Status != shadowMCPRequestStatusRequested {
		return nil, oops.E(oops.CodeConflict, nil, "shadow mcp approval request has already been decided").Log(ctx, s.logger)
	}
	requestProjectID, err := uuid.Parse(request.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse shadow mcp approval request project id").Log(ctx, s.logger)
	}
	ruleProjectIDs, err := s.shadowMCPRuleProjectIDs(ctx, ac.ActiveOrganizationID, payload.AccessScope, payload.ProjectIds, nil, requestProjectID)
	if err != nil {
		return nil, err
	}

	requestBefore := buildShadowMCPApprovalRequest(request)
	requestSummary := shadowMCPSummaryFromAccessControl(request.ObservedSummary)
	sourceRules := make([]accesscontrol.AccessRule, 0, len(ruleProjectIDs))
	now := time.Now().UTC()
	for _, ruleProjectID := range ruleProjectIDs {
		sourceRules = append(sourceRules, accesscontrol.AccessRule{
			ID:             "",
			OrganizationID: ac.ActiveOrganizationID,
			ProjectID:      nullUUIDToString(ruleProjectID),
			AccessScope:    payload.AccessScope,
			ResourceType:   shadowMCPResourceType,
			Disposition:    shadowMCPRuleAllowed,
			MatchKind:      payload.MatchBreadth,
			MatchValue:     matchValue,
			DisplayName:    payload.DisplayName,
			ObservedSummary: accessControlSummaryFromShadowMCPSummary(shadowMCPSummaryFromRulePayload(
				coalesceString(payload.ObservedFullURL, requestSummary.FullURL),
				coalesceString(payload.ObservedURLHost, requestSummary.URLHost),
				coalesceString(payload.ObservedServerIdentity, requestSummary.ServerIdentity),
			)),
			SourceRequestID: requestID.String(),
			CreatedBy:       ac.UserID,
			UpdatedBy:       ac.UserID,
			Reason:          conv.PtrValOr(payload.Reason, ""),
			CreatedAt:       now,
			UpdatedAt:       now,
		})
	}

	request, ruleResults, err := s.accessStore.DecideRequestWithRules(ctx, ac.ActiveOrganizationID, shadowMCPResourceType, request.ID, shadowMCPRequestStatusApproved, ac.UserID, conv.PtrValOr(payload.Reason, ""), sourceRules)
	if err != nil {
		return nil, shadowMCPStoreErrWithConflict(ctx, s, err, "approve shadow mcp approval request", "shadow mcp access rule already exists")
	}
	requestAfter := buildShadowMCPApprovalRequest(request)
	ruleViews := make([]*gen.ShadowMCPAccessRule, 0, len(ruleResults))
	ruleAuditEvents := make([]shadowMCPAccessRuleAuditEvent, 0, len(ruleResults))
	for _, ruleResult := range ruleResults {
		var ruleBefore *gen.ShadowMCPAccessRule
		if !ruleResult.Created {
			ruleBefore = buildShadowMCPAccessRule(ruleResult.Rule)
		}
		ruleAfter := buildShadowMCPAccessRule(ruleResult.Rule)
		ruleUUID, err := uuid.Parse(ruleResult.Rule.ID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse shadow mcp access rule id").Log(ctx, s.logger)
		}
		ruleAuditEvents = append(ruleAuditEvents, shadowMCPAccessRuleAuditEvent{
			Created: ruleResult.Created,
			Event: audit.LogShadowMCPAccessRuleEvent{
				OrganizationID:           ac.ActiveOrganizationID,
				ProjectID:                shadowMCPAuditProjectID(ruleResult.Rule.ProjectID),
				Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
				ActorDisplayName:         ac.Email,
				ActorSlug:                nil,
				AccessRuleURN:            urn.NewShadowMCPAccessRule(ruleUUID),
				DisplayName:              ruleResult.Rule.DisplayName,
				MatchValue:               ruleResult.Rule.MatchValue,
				AccessRuleSnapshotBefore: ruleBefore,
				AccessRuleSnapshotAfter:  ruleAfter,
				Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
			},
		})
		ruleViews = append(ruleViews, ruleAfter)
	}
	s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp approval audit events", func(dbtx pgx.Tx) error {
		for _, ruleAudit := range ruleAuditEvents {
			if ruleAudit.Created {
				if err := s.audit.LogShadowMCPAccessRuleCreate(ctx, dbtx, ruleAudit.Event); err != nil {
					return fmt.Errorf("log shadow mcp access rule create: %w", err)
				}
			} else {
				if err := s.audit.LogShadowMCPAccessRuleUpdate(ctx, dbtx, ruleAudit.Event); err != nil {
					return fmt.Errorf("log shadow mcp access rule update: %w", err)
				}
			}
		}
		return s.audit.LogShadowMCPApprovalRequestApprove(ctx, dbtx, audit.LogShadowMCPApprovalRequestEvent{
			OrganizationID:                ac.ActiveOrganizationID,
			ProjectID:                     requestProjectID,
			Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:              ac.Email,
			ActorSlug:                     nil,
			ApprovalRequestURN:            urn.NewShadowMCPApprovalRequest(requestID),
			DisplayName:                   shadowMCPApprovalRequestDisplayName(request),
			ApprovalRequestSnapshotBefore: requestBefore,
			ApprovalRequestSnapshotAfter:  requestAfter,
			Metadata:                      &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
		})
	})

	ruleView := firstShadowMCPAccessRule(ruleViews)
	return &gen.ShadowMCPApprovalDecisionResult{
		Request: requestAfter,
		Rule:    ruleView,
		Rules:   ruleViews,
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

	request, err := s.accessStore.GetRequest(ctx, ac.ActiveOrganizationID, shadowMCPResourceType, payload.ID)
	if err != nil {
		return nil, shadowMCPStoreErr(ctx, s, err, "get shadow mcp approval request")
	}
	if request.Status != shadowMCPRequestStatusRequested {
		return nil, oops.E(oops.CodeConflict, nil, "shadow mcp approval request has already been decided").Log(ctx, s.logger)
	}
	requestProjectID, err := uuid.Parse(request.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse shadow mcp approval request project id").Log(ctx, s.logger)
	}

	ruleViews := []*gen.ShadowMCPAccessRule{}
	ruleAuditEvents := make([]audit.LogShadowMCPAccessRuleEvent, 0)
	sourceRules := make([]accesscontrol.AccessRule, 0)
	requestSummary := shadowMCPSummaryFromAccessControl(request.ObservedSummary)
	if payload.CreateDenyRule {
		matchValue, err := normalizeShadowMCPMatchValue(*payload.MatchBreadth, *payload.MatchValue)
		if err != nil {
			return nil, err
		}
		ruleProjectIDs, err := s.shadowMCPRuleProjectIDs(ctx, ac.ActiveOrganizationID, shadowMCPAccessScopeProject, payload.ProjectIds, nil, requestProjectID)
		if err != nil {
			return nil, err
		}
		sourceRules = make([]accesscontrol.AccessRule, 0, len(ruleProjectIDs))
		now := time.Now().UTC()
		for _, ruleProjectID := range ruleProjectIDs {
			sourceRules = append(sourceRules, accesscontrol.AccessRule{
				ID:             "",
				OrganizationID: ac.ActiveOrganizationID,
				ProjectID:      nullUUIDToString(ruleProjectID),
				AccessScope:    shadowMCPAccessScopeProject,
				ResourceType:   shadowMCPResourceType,
				Disposition:    shadowMCPRuleDenied,
				MatchKind:      *payload.MatchBreadth,
				MatchValue:     matchValue,
				DisplayName:    *payload.DisplayName,
				ObservedSummary: accessControlSummaryFromShadowMCPSummary(shadowMCPSummaryFromRulePayload(
					coalesceString(payload.ObservedFullURL, requestSummary.FullURL),
					coalesceString(payload.ObservedURLHost, requestSummary.URLHost),
					coalesceString(payload.ObservedServerIdentity, requestSummary.ServerIdentity),
				)),
				SourceRequestID: requestID.String(),
				CreatedBy:       ac.UserID,
				UpdatedBy:       ac.UserID,
				Reason:          conv.PtrValOr(payload.Reason, ""),
				CreatedAt:       now,
				UpdatedAt:       now,
			})
		}
	}

	requestBefore := buildShadowMCPApprovalRequest(request)
	var ruleResults []accesscontrol.RuleUpsertResult
	if payload.CreateDenyRule {
		request, ruleResults, err = s.accessStore.DecideRequestWithRules(ctx, ac.ActiveOrganizationID, shadowMCPResourceType, request.ID, shadowMCPRequestStatusDenied, ac.UserID, conv.PtrValOr(payload.Reason, ""), sourceRules)
		if err != nil {
			return nil, shadowMCPStoreErrWithConflict(ctx, s, err, "deny shadow mcp approval request", "shadow mcp access rule already exists")
		}
	} else {
		request, err = s.accessStore.DecideRequest(ctx, ac.ActiveOrganizationID, shadowMCPResourceType, request.ID, shadowMCPRequestStatusDenied, ac.UserID, conv.PtrValOr(payload.Reason, ""), nil)
		if err != nil {
			return nil, shadowMCPStoreErr(ctx, s, err, "deny shadow mcp approval request")
		}
	}
	ruleViews = make([]*gen.ShadowMCPAccessRule, 0, len(ruleResults))
	ruleAuditEvents = make([]audit.LogShadowMCPAccessRuleEvent, 0, len(ruleResults))
	for _, ruleResult := range ruleResults {
		ruleAfter := buildShadowMCPAccessRule(ruleResult.Rule)
		if ruleResult.Created {
			ruleUUID, err := uuid.Parse(ruleResult.Rule.ID)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "parse shadow mcp access rule id").Log(ctx, s.logger)
			}
			ruleAuditEvents = append(ruleAuditEvents, audit.LogShadowMCPAccessRuleEvent{
				OrganizationID:           ac.ActiveOrganizationID,
				ProjectID:                shadowMCPAuditProjectID(ruleResult.Rule.ProjectID),
				Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
				ActorDisplayName:         ac.Email,
				ActorSlug:                nil,
				AccessRuleURN:            urn.NewShadowMCPAccessRule(ruleUUID),
				DisplayName:              ruleResult.Rule.DisplayName,
				MatchValue:               ruleResult.Rule.MatchValue,
				AccessRuleSnapshotBefore: nil,
				AccessRuleSnapshotAfter:  ruleAfter,
				Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
			})
		}
		ruleViews = append(ruleViews, ruleAfter)
	}
	requestAfter := buildShadowMCPApprovalRequest(request)
	s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp denial audit events", func(dbtx pgx.Tx) error {
		for _, ruleAuditEvent := range ruleAuditEvents {
			if err := s.audit.LogShadowMCPAccessRuleCreate(ctx, dbtx, ruleAuditEvent); err != nil {
				return fmt.Errorf("log shadow mcp access rule create: %w", err)
			}
		}
		return s.audit.LogShadowMCPApprovalRequestDeny(ctx, dbtx, audit.LogShadowMCPApprovalRequestEvent{
			OrganizationID:                ac.ActiveOrganizationID,
			ProjectID:                     requestProjectID,
			Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:              ac.Email,
			ActorSlug:                     nil,
			ApprovalRequestURN:            urn.NewShadowMCPApprovalRequest(requestID),
			DisplayName:                   shadowMCPApprovalRequestDisplayName(request),
			ApprovalRequestSnapshotBefore: requestBefore,
			ApprovalRequestSnapshotAfter:  requestAfter,
			Metadata:                      &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
		})
	})

	return &gen.ShadowMCPApprovalDecisionResult{
		Request: requestAfter,
		Rule:    firstShadowMCPAccessRule(ruleViews),
		Rules:   ruleViews,
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
	cursor, err := decodeShadowMCPCursorParam(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
	}
	result, err := s.accessStore.ListRules(ctx, accesscontrol.RuleFilters{
		OrganizationID: ac.ActiveOrganizationID,
		ResourceType:   shadowMCPResourceType,
		Disposition:    disposition,
		AccessScope:    accessScope,
		ProjectID:      projectID,
		Cursor:         cursor,
		Limit:          limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list shadow mcp access rules").Log(ctx, s.logger)
	}

	var nextCursor *string
	if result.NextCursor != "" {
		cursor := encodeShadowMCPCursor(result.NextCursor)
		nextCursor = &cursor
	}

	rules := make([]*gen.ShadowMCPAccessRule, 0, len(result.Rules))
	for _, row := range result.Rules {
		rules = append(rules, buildShadowMCPAccessRule(row))
	}

	return &gen.ListShadowMCPAccessRulesResult{
		Rules:      rules,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) CreateShadowMCPAccessRule(ctx context.Context, payload *gen.CreateShadowMCPAccessRulePayload) (*gen.CreateShadowMCPAccessRuleResult, error) {
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
	ruleProjectIDs, err := s.shadowMCPRuleProjectIDs(ctx, ac.ActiveOrganizationID, payload.AccessScope, payload.ProjectIds, payload.ProjectID, uuid.Nil)
	if err != nil {
		return nil, err
	}

	rules := make([]accesscontrol.AccessRule, 0, len(ruleProjectIDs))
	now := time.Now().UTC()
	for _, ruleProjectID := range ruleProjectIDs {
		rules = append(rules, accesscontrol.AccessRule{
			ID:              "",
			OrganizationID:  ac.ActiveOrganizationID,
			ProjectID:       nullUUIDToString(ruleProjectID),
			AccessScope:     payload.AccessScope,
			ResourceType:    shadowMCPResourceType,
			Disposition:     payload.Disposition,
			MatchKind:       payload.MatchBreadth,
			MatchValue:      matchValue,
			DisplayName:     payload.DisplayName,
			ObservedSummary: accessControlSummaryFromShadowMCPSummary(shadowMCPSummaryFromRulePayload(payload.ObservedFullURL, payload.ObservedURLHost, payload.ObservedServerIdentity)),
			SourceRequestID: "",
			CreatedBy:       ac.UserID,
			UpdatedBy:       ac.UserID,
			Reason:          conv.PtrValOr(payload.Reason, ""),
			CreatedAt:       now,
			UpdatedAt:       now,
		})
	}
	createdRules, err := s.accessStore.CreateRules(ctx, rules)
	if err != nil {
		return nil, shadowMCPStoreErrWithConflict(ctx, s, err, "create shadow mcp access rule", "shadow mcp access rule already exists")
	}

	ruleViews := make([]*gen.ShadowMCPAccessRule, 0, len(createdRules))
	ruleAuditEvents := make([]audit.LogShadowMCPAccessRuleEvent, 0, len(createdRules))
	for _, rule := range createdRules {
		ruleView := buildShadowMCPAccessRule(rule)
		ruleUUID, err := uuid.Parse(rule.ID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "parse shadow mcp access rule id").Log(ctx, s.logger)
		}
		ruleAuditEvents = append(ruleAuditEvents, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           ac.ActiveOrganizationID,
			ProjectID:                shadowMCPAuditProjectID(rule.ProjectID),
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:         ac.Email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(ruleUUID),
			DisplayName:              rule.DisplayName,
			MatchValue:               rule.MatchValue,
			AccessRuleSnapshotBefore: nil,
			AccessRuleSnapshotAfter:  ruleView,
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
		})
		ruleViews = append(ruleViews, ruleView)
	}
	s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp access rule create audit events", func(dbtx pgx.Tx) error {
		for _, ruleAuditEvent := range ruleAuditEvents {
			if err := s.audit.LogShadowMCPAccessRuleCreate(ctx, dbtx, ruleAuditEvent); err != nil {
				return fmt.Errorf("log shadow mcp access rule create: %w", err)
			}
		}
		return nil
	})

	return &gen.CreateShadowMCPAccessRuleResult{Rules: ruleViews}, nil
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

	existingRule, err := s.accessStore.GetRule(ctx, ac.ActiveOrganizationID, shadowMCPResourceType, payload.ID)
	if err != nil {
		return nil, shadowMCPStoreErr(ctx, s, err, "get shadow mcp access rule")
	}
	rule, err := s.accessStore.UpdateRule(ctx, accesscontrol.AccessRule{
		ID:              payload.ID,
		OrganizationID:  ac.ActiveOrganizationID,
		Disposition:     payload.Disposition,
		ProjectID:       nullUUIDToString(ruleProjectID),
		AccessScope:     payload.AccessScope,
		ResourceType:    shadowMCPResourceType,
		MatchKind:       payload.MatchBreadth,
		MatchValue:      matchValue,
		DisplayName:     payload.DisplayName,
		ObservedSummary: accessControlSummaryFromShadowMCPSummary(shadowMCPSummaryFromRulePayload(payload.ObservedFullURL, payload.ObservedURLHost, payload.ObservedServerIdentity)),
		SourceRequestID: existingRule.SourceRequestID,
		CreatedBy:       existingRule.CreatedBy,
		UpdatedBy:       ac.UserID,
		Reason:          conv.PtrValOr(payload.Reason, ""),
		CreatedAt:       existingRule.CreatedAt,
		UpdatedAt:       time.Now().UTC(),
	})
	if err != nil {
		return nil, shadowMCPStoreErrWithConflict(ctx, s, err, "update shadow mcp access rule", "shadow mcp access rule already exists")
	}
	ruleBefore := buildShadowMCPAccessRule(existingRule)
	ruleAfter := buildShadowMCPAccessRule(rule)
	s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp access rule update", func(dbtx pgx.Tx) error {
		return s.audit.LogShadowMCPAccessRuleUpdate(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           ac.ActiveOrganizationID,
			ProjectID:                shadowMCPAuditProjectID(rule.ProjectID),
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:         ac.Email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(ruleID),
			DisplayName:              rule.DisplayName,
			MatchValue:               rule.MatchValue,
			AccessRuleSnapshotBefore: ruleBefore,
			AccessRuleSnapshotAfter:  ruleAfter,
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
		})
	})

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

	rule, err := s.accessStore.DeleteRule(ctx, ac.ActiveOrganizationID, shadowMCPResourceType, payload.ID)
	if err != nil {
		return shadowMCPStoreErr(ctx, s, err, "delete shadow mcp access rule")
	}
	s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp access rule delete", func(dbtx pgx.Tx) error {
		return s.audit.LogShadowMCPAccessRuleDelete(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           ac.ActiveOrganizationID,
			ProjectID:                shadowMCPAuditProjectID(rule.ProjectID),
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:         ac.Email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(ruleID),
			DisplayName:              rule.DisplayName,
			MatchValue:               rule.MatchValue,
			AccessRuleSnapshotBefore: buildShadowMCPAccessRule(rule),
			AccessRuleSnapshotAfter:  nil,
			Metadata:                 nil,
		})
	})

	return nil
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

type shadowMCPObservedSummary struct {
	Name           *string `json:"name,omitempty"`
	FullURL        *string `json:"full_url,omitempty"`
	URLHost        *string `json:"url_host,omitempty"`
	ServerIdentity *string `json:"server_identity,omitempty"`
	ToolName       *string `json:"tool_name,omitempty"`
	ToolCall       *string `json:"tool_call,omitempty"`
	BlockReason    *string `json:"block_reason,omitempty"`
	RiskPolicyID   *string `json:"risk_policy_id,omitempty"`
	RiskResultID   *string `json:"risk_result_id,omitempty"`
}

func shadowMCPEmptySummary() shadowMCPObservedSummary {
	return shadowMCPObservedSummary{
		Name:           nil,
		FullURL:        nil,
		URLHost:        nil,
		ServerIdentity: nil,
		ToolName:       nil,
		ToolCall:       nil,
		BlockReason:    nil,
		RiskPolicyID:   nil,
		RiskResultID:   nil,
	}
}

func accessControlSummaryFromShadowMCPSummary(summary shadowMCPObservedSummary) accesscontrol.ObservedSummary {
	return accesscontrol.ObservedSummary{
		Name:           summary.Name,
		FullURL:        summary.FullURL,
		URLHost:        summary.URLHost,
		ServerIdentity: summary.ServerIdentity,
		ToolName:       summary.ToolName,
		ToolCall:       summary.ToolCall,
		BlockReason:    summary.BlockReason,
		RiskPolicyID:   summary.RiskPolicyID,
		RiskResultID:   summary.RiskResultID,
	}
}

func shadowMCPSummaryFromAccessControl(summary accesscontrol.ObservedSummary) shadowMCPObservedSummary {
	return shadowMCPObservedSummary{
		Name:           summary.Name,
		FullURL:        summary.FullURL,
		URLHost:        summary.URLHost,
		ServerIdentity: summary.ServerIdentity,
		ToolName:       summary.ToolName,
		ToolCall:       summary.ToolCall,
		BlockReason:    summary.BlockReason,
		RiskPolicyID:   summary.RiskPolicyID,
		RiskResultID:   summary.RiskResultID,
	}
}

func shadowMCPSummaryFromClaims(claims *shadowMCPApprovalRequestClaims) shadowMCPObservedSummary {
	return shadowMCPObservedSummary{
		Name:           claims.ObservedName,
		FullURL:        claims.ObservedFullURL,
		URLHost:        claims.ObservedURLHost,
		ServerIdentity: claims.ObservedServerIdentity,
		ToolName:       claims.ToolName,
		ToolCall:       claims.ToolCall,
		BlockReason:    claims.BlockReason,
		RiskPolicyID:   claims.RiskPolicyID,
		RiskResultID:   claims.RiskResultID,
	}
}

func shadowMCPSummaryFromRulePayload(fullURL, urlHost, serverIdentity *string) shadowMCPObservedSummary {
	return shadowMCPObservedSummary{
		Name:           nil,
		FullURL:        fullURL,
		URLHost:        urlHost,
		ServerIdentity: serverIdentity,
		ToolName:       nil,
		ToolCall:       nil,
		BlockReason:    nil,
		RiskPolicyID:   nil,
		RiskResultID:   nil,
	}
}

func shadowMCPSummaryDisplayName(summary shadowMCPObservedSummary, fallback string) string {
	for _, value := range []*string{summary.Name, summary.FullURL, summary.URLHost, summary.ServerIdentity} {
		if value != nil && *value != "" {
			return *value
		}
	}
	return fallback
}

func shadowMCPAuditProjectID(projectID string) uuid.UUID {
	if projectID != "" {
		parsed, err := uuid.Parse(projectID)
		if err == nil {
			return parsed
		}
	}
	return uuid.Nil
}

func buildShadowMCPApprovalRequest(row accesscontrol.AccessApprovalRequest) *gen.ShadowMCPApprovalRequest {
	summary := shadowMCPSummaryFromAccessControl(row.ObservedSummary)
	return &gen.ShadowMCPApprovalRequest{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		ProjectID:              row.ProjectID,
		RequesterUserID:        conv.PtrEmpty(row.RequesterUserID),
		RequesterEmail:         conv.PtrEmpty(row.RequesterEmail),
		RequesterDisplayName:   conv.PtrEmpty(row.RequesterDisplayName),
		Status:                 row.Status,
		RiskPolicyID:           summary.RiskPolicyID,
		RiskResultID:           summary.RiskResultID,
		ObservedName:           summary.Name,
		ObservedFullURL:        summary.FullURL,
		ObservedURLHost:        summary.URLHost,
		ObservedServerIdentity: summary.ServerIdentity,
		ToolName:               summary.ToolName,
		ToolCall:               summary.ToolCall,
		BlockReason:            summary.BlockReason,
		BlockedCount:           row.BlockedCount,
		FirstBlockedAt:         formatTimePtr(row.FirstBlockedAt),
		LastBlockedAt:          formatTimePtr(row.LastBlockedAt),
		RequestedAt:            formatTimeValue(row.RequestedAt),
		DecidedAt:              formatTimePtrValue(row.DecidedAt),
		DecidedBy:              conv.PtrEmpty(row.DecidedBy),
		DecisionNote:           conv.PtrEmpty(row.DecisionNote),
		CreatedAt:              formatTimeValue(row.CreatedAt),
		UpdatedAt:              formatTimeValue(row.UpdatedAt),
	}
}

func shadowMCPApprovalRequestDisplayName(row accesscontrol.AccessApprovalRequest) string {
	if row.DisplayName != "" {
		return row.DisplayName
	}
	return shadowMCPSummaryDisplayName(shadowMCPSummaryFromAccessControl(row.ObservedSummary), row.ID)
}

func buildShadowMCPAccessRule(row accesscontrol.AccessRule) *gen.ShadowMCPAccessRule {
	summary := shadowMCPSummaryFromAccessControl(row.ObservedSummary)
	return &gen.ShadowMCPAccessRule{
		ID:                     row.ID,
		OrganizationID:         row.OrganizationID,
		ProjectID:              conv.PtrEmpty(row.ProjectID),
		AccessScope:            row.AccessScope,
		Disposition:            row.Disposition,
		MatchBreadth:           row.MatchKind,
		MatchValue:             row.MatchValue,
		DisplayName:            row.DisplayName,
		ObservedFullURL:        summary.FullURL,
		ObservedURLHost:        summary.URLHost,
		ObservedServerIdentity: summary.ServerIdentity,
		SourceRequestID:        conv.PtrEmpty(row.SourceRequestID),
		CreatedBy:              conv.PtrEmpty(row.CreatedBy),
		UpdatedBy:              conv.PtrEmpty(row.UpdatedBy),
		Reason:                 conv.PtrEmpty(row.Reason),
		CreatedAt:              formatTimeValue(row.CreatedAt),
		UpdatedAt:              formatTimeValue(row.UpdatedAt),
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
	return accesscontrol.CanonicalizeMatchValue(matchBreadth, value), nil
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

func (s *Service) shadowMCPRuleProjectIDs(ctx context.Context, organizationID string, accessScope string, projectIDs []string, projectID *string, fallbackProjectID uuid.UUID) ([]uuid.NullUUID, error) {
	switch accessScope {
	case shadowMCPAccessScopeOrganization:
		return []uuid.NullUUID{{UUID: uuid.Nil, Valid: false}}, nil
	case shadowMCPAccessScopeProject:
		ids := make([]uuid.UUID, 0, max(len(projectIDs), 1))
		seen := map[uuid.UUID]struct{}{}
		for _, rawID := range projectIDs {
			if rawID == "" {
				continue
			}
			parsed, err := uuid.Parse(rawID)
			if err != nil {
				return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
			}
			if _, ok := seen[parsed]; ok {
				continue
			}
			seen[parsed] = struct{}{}
			ids = append(ids, parsed)
		}
		if len(ids) == 0 && projectID != nil && *projectID != "" {
			parsed, err := uuid.Parse(*projectID)
			if err != nil {
				return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").Log(ctx, s.logger)
			}
			ids = append(ids, parsed)
		}
		if len(ids) == 0 && fallbackProjectID != uuid.Nil {
			ids = append(ids, fallbackProjectID)
		}
		if len(ids) == 0 {
			return nil, oops.E(oops.CodeBadRequest, nil, "project_id is required for project-scoped shadow mcp access rules").Log(ctx, s.logger)
		}

		ruleProjectIDs := make([]uuid.NullUUID, 0, len(ids))
		for _, id := range ids {
			if err := s.requireProjectInOrganization(ctx, organizationID, id); err != nil {
				return nil, err
			}
			ruleProjectIDs = append(ruleProjectIDs, uuid.NullUUID{UUID: id, Valid: true})
		}
		return ruleProjectIDs, nil
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid access_scope").Log(ctx, s.logger)
	}
}

func firstShadowMCPAccessRule(rules []*gen.ShadowMCPAccessRule) *gen.ShadowMCPAccessRule {
	if len(rules) == 0 {
		return nil
	}
	return rules[0]
}

func (s *Service) logShadowMCPAuditBestEffort(ctx context.Context, operation string, write func(pgx.Tx) error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, operation, attr.SlogError(fmt.Errorf("begin audit transaction: %w", err)))
		return
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := write(dbtx); err != nil {
		s.logger.WarnContext(ctx, operation, attr.SlogError(err))
		return
	}
	if err := dbtx.Commit(ctx); err != nil {
		s.logger.WarnContext(ctx, operation, attr.SlogError(fmt.Errorf("commit audit transaction: %w", err)))
	}
}

func shadowMCPStoreErr(ctx context.Context, s *Service, err error, message string) error {
	return shadowMCPStoreErrWithConflict(ctx, s, err, message, message)
}

func shadowMCPStoreErrWithConflict(ctx context.Context, s *Service, err error, message, conflictMessage string) error {
	if errors.Is(err, accesscontrol.ErrNotFound) {
		return oops.E(oops.CodeNotFound, nil, "%s", message).Log(ctx, s.logger)
	}
	if errors.Is(err, accesscontrol.ErrRequestAlreadyDecided) {
		return oops.E(oops.CodeConflict, nil, "shadow mcp approval request has already been decided").Log(ctx, s.logger)
	}
	if errors.Is(err, accesscontrol.ErrConflict) {
		return oops.E(oops.CodeConflict, nil, "%s", conflictMessage).Log(ctx, s.logger)
	}
	return oops.E(oops.CodeUnexpected, err, "%s", message).Log(ctx, s.logger)
}

func formatTimePtr(ts time.Time) *string {
	if ts.IsZero() {
		return nil
	}
	value := ts.UTC().Format(time.RFC3339)
	return &value
}

func formatTimePtrValue(ts *time.Time) *string {
	if ts == nil {
		return nil
	}
	return formatTimePtr(*ts)
}

func formatTimeValue(ts time.Time) string {
	if ts.IsZero() {
		return time.Time{}.UTC().Format(time.RFC3339)
	}
	return ts.UTC().Format(time.RFC3339)
}

func shadowMCPLimit(limit int) (int, error) {
	if limit < 1 {
		return 0, oops.E(oops.CodeBadRequest, nil, "limit must be greater than or equal to 1")
	}
	if limit > shadowMCPMaxPageLimit {
		return 0, oops.E(oops.CodeBadRequest, nil, "limit must be less than or equal to 1000")
	}
	return limit, nil
}

func encodeShadowMCPCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func decodeShadowMCPCursorParam(cursor *string) (string, error) {
	if cursor == nil || *cursor == "" {
		return "", nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return "", fmt.Errorf("decode cursor: %w", err)
	}
	value := string(decoded)
	if parts := strings.SplitN(value, ":", 2); len(parts) == 2 {
		value = parts[1]
	}
	if _, err := uuid.Parse(value); err != nil {
		return "", fmt.Errorf("parse cursor id: %w", err)
	}
	return value, nil
}

func coalesceString(primary *string, fallback *string) *string {
	if primary != nil {
		return primary
	}
	return fallback
}

func nullUUIDToString(id uuid.NullUUID) string {
	if !id.Valid {
		return ""
	}
	return id.UUID.String()
}

type shadowMCPAccessRuleAuditEvent struct {
	Created bool
	Event   audit.LogShadowMCPAccessRuleEvent
}
