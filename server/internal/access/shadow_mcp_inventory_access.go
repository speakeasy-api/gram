package access

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type shadowMCPInventoryServerAccessForm struct {
	ProjectID   string
	ServerURL   string
	ServerName  *string
	Reason      *string
	Disposition string
}

type shadowMCPInventoryServerAccessRuleSetResult struct {
	Rule        accesscontrol.AccessRule
	Created     bool
	UpdatedFrom *accesscontrol.AccessRule
}

const shadowMCPInventoryBatchAllowMaxSize = 200

func (s *Service) AllowShadowMCPInventoryServer(ctx context.Context, payload *gen.AllowShadowMCPInventoryServerPayload) (*gen.ShadowMCPInventoryAccessState, error) {
	return s.setShadowMCPInventoryServerAccess(ctx, shadowMCPInventoryServerAccessForm{
		ProjectID:   payload.ProjectID,
		ServerURL:   payload.ServerURL,
		ServerName:  payload.ServerName,
		Reason:      payload.Reason,
		Disposition: accesscontrol.DispositionAllowed,
	})
}

func (s *Service) BatchAllowShadowMCPInventoryServers(ctx context.Context, payload *gen.BatchAllowShadowMCPInventoryServersPayload) (*gen.BatchAllowShadowMCPInventoryServersResult, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if len(payload.Servers) > shadowMCPInventoryBatchAllowMaxSize {
		return nil, oops.E(oops.CodeBadRequest, nil, "batch allow supports at most %d shadow mcp server urls", shadowMCPInventoryBatchAllowMaxSize).LogError(ctx, s.logger)
	}

	projectID, err := uuid.Parse(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, s.logger)
	}
	if err := s.requireProjectInOrganization(ctx, ac.ActiveOrganizationID, projectID); err != nil {
		return nil, err
	}

	results := make([]*gen.ShadowMCPInventoryBatchAllowResult, 0, len(payload.Servers))
	for _, server := range payload.Servers {
		result := &gen.ShadowMCPInventoryBatchAllowResult{
			ServerURL:    "",
			Success:      false,
			AccessState:  nil,
			ErrorCode:    nil,
			ErrorMessage: nil,
		}
		if server == nil {
			code := string(oops.CodeBadRequest)
			message := "server is required"
			result.ErrorCode = &code
			result.ErrorMessage = &message
			results = append(results, result)
			continue
		}
		result.ServerURL = server.ServerURL

		inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(server.ServerURL)
		if !ok {
			code := "invalid_url"
			message := "invalid shadow mcp server url"
			result.ErrorCode = &code
			result.ErrorMessage = &message
			results = append(results, result)
			continue
		}

		state, err := s.setShadowMCPInventoryServerAccessForTarget(ctx, ac, projectID, inventoryURL, shadowMCPInventoryServerAccessForm{
			ProjectID:   payload.ProjectID,
			ServerURL:   server.ServerURL,
			ServerName:  server.ServerName,
			Reason:      payload.Reason,
			Disposition: accesscontrol.DispositionAllowed,
		})
		if err != nil {
			code, message := shadowMCPInventoryBatchAllowError(err)
			result.ErrorCode = &code
			result.ErrorMessage = &message
			results = append(results, result)
			continue
		}

		result.Success = true
		result.AccessState = state
		results = append(results, result)
	}

	return &gen.BatchAllowShadowMCPInventoryServersResult{Results: results}, nil
}

func (s *Service) BlockShadowMCPInventoryServer(ctx context.Context, payload *gen.BlockShadowMCPInventoryServerPayload) (*gen.ShadowMCPInventoryAccessState, error) {
	return s.setShadowMCPInventoryServerAccess(ctx, shadowMCPInventoryServerAccessForm{
		ProjectID:   payload.ProjectID,
		ServerURL:   payload.ServerURL,
		ServerName:  payload.ServerName,
		Reason:      payload.Reason,
		Disposition: accesscontrol.DispositionDenied,
	})
}

func (s *Service) ClearShadowMCPInventoryServerAccess(ctx context.Context, payload *gen.ClearShadowMCPInventoryServerAccessPayload) (*gen.ShadowMCPInventoryAccessState, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	projectID, inventoryURL, err := s.parseShadowMCPInventoryServerAccessTarget(ctx, ac.ActiveOrganizationID, payload.ProjectID, payload.ServerURL)
	if err != nil {
		return nil, err
	}

	rule, err := s.accessStore.GetRuleByMatch(
		ctx,
		ac.ActiveOrganizationID,
		accesscontrol.ResourceTypeShadowMCP,
		accesscontrol.AccessScopeProject,
		projectID.String(),
		accesscontrol.MatchKindFullURL,
		inventoryURL.CanonicalURL,
	)
	switch {
	case errors.Is(err, accesscontrol.ErrNotFound):
		return s.buildShadowMCPInventoryAccessState(ctx, ac.ActiveOrganizationID, projectID.String(), inventoryURL)
	case err != nil:
		return nil, shadowMCPStoreErr(ctx, s, err, "get shadow mcp inventory access rule")
	}

	deletedRule, err := s.accessStore.DeleteRule(ctx, ac.ActiveOrganizationID, accesscontrol.ResourceTypeShadowMCP, rule.ID)
	if errors.Is(err, accesscontrol.ErrNotFound) {
		return s.buildShadowMCPInventoryAccessState(ctx, ac.ActiveOrganizationID, projectID.String(), inventoryURL)
	}
	if err != nil {
		return nil, shadowMCPStoreErr(ctx, s, err, "clear shadow mcp inventory access rule")
	}
	ruleID, err := uuid.Parse(deletedRule.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "parse shadow mcp access rule id").LogError(ctx, s.logger)
	}
	s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp inventory access rule delete", func(dbtx pgx.Tx) error {
		return s.audit.LogShadowMCPAccessRuleDelete(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           ac.ActiveOrganizationID,
			ProjectID:                projectID,
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			ActorDisplayName:         ac.Email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(ruleID),
			DisplayName:              deletedRule.DisplayName,
			MatchValue:               deletedRule.MatchValue,
			AccessRuleSnapshotBefore: buildShadowMCPAccessRule(deletedRule),
			AccessRuleSnapshotAfter:  nil,
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: payload.Reason},
		})
	})

	return s.buildShadowMCPInventoryAccessState(ctx, ac.ActiveOrganizationID, projectID.String(), inventoryURL)
}

func (s *Service) setShadowMCPInventoryServerAccess(ctx context.Context, form shadowMCPInventoryServerAccessForm) (*gen.ShadowMCPInventoryAccessState, error) {
	ac, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}

	projectID, inventoryURL, err := s.parseShadowMCPInventoryServerAccessTarget(ctx, ac.ActiveOrganizationID, form.ProjectID, form.ServerURL)
	if err != nil {
		return nil, err
	}

	return s.setShadowMCPInventoryServerAccessForTarget(ctx, ac, projectID, inventoryURL, form)
}

func (s *Service) setShadowMCPInventoryServerAccessForTarget(ctx context.Context, ac *contextvalues.AuthContext, projectID uuid.UUID, inventoryURL shadowmcp.InventoryURL, form shadowMCPInventoryServerAccessForm) (*gen.ShadowMCPInventoryAccessState, error) {
	rule := buildShadowMCPInventoryServerAccessRule(ac.ActiveOrganizationID, ac.UserID, projectID.String(), inventoryURL, form)
	existingRule, err := s.accessStore.GetRuleByMatch(
		ctx,
		ac.ActiveOrganizationID,
		accesscontrol.ResourceTypeShadowMCP,
		accesscontrol.AccessScopeProject,
		projectID.String(),
		accesscontrol.MatchKindFullURL,
		inventoryURL.CanonicalURL,
	)
	switch {
	case errors.Is(err, accesscontrol.ErrNotFound):
		result, err := s.createShadowMCPInventoryServerAccessRule(ctx, rule)
		if err != nil {
			return nil, err
		}
		switch {
		case result.Created:
			if err := s.logShadowMCPInventoryAccessRuleCreate(ctx, ac.ActiveOrganizationID, ac.UserID, ac.Email, projectID, form.Reason, result.Rule); err != nil {
				return nil, err
			}
		case result.UpdatedFrom != nil:
			if err := s.logShadowMCPInventoryAccessRuleUpdate(ctx, ac.ActiveOrganizationID, ac.UserID, ac.Email, projectID, form.Reason, *result.UpdatedFrom, result.Rule); err != nil {
				return nil, err
			}
		}
	case err != nil:
		return nil, shadowMCPStoreErr(ctx, s, err, "get shadow mcp inventory access rule")
	case existingRule.Disposition == form.Disposition:
		return s.buildShadowMCPInventoryAccessState(ctx, ac.ActiveOrganizationID, projectID.String(), inventoryURL)
	default:
		updatedRule, err := s.updateShadowMCPInventoryServerAccessRule(ctx, ac.UserID, existingRule, rule)
		if err != nil {
			return nil, err
		}
		if err := s.logShadowMCPInventoryAccessRuleUpdate(ctx, ac.ActiveOrganizationID, ac.UserID, ac.Email, projectID, form.Reason, existingRule, updatedRule); err != nil {
			return nil, err
		}
	}

	return s.buildShadowMCPInventoryAccessState(ctx, ac.ActiveOrganizationID, projectID.String(), inventoryURL)
}

func shadowMCPInventoryBatchAllowError(err error) (string, string) {
	var oopsErr *oops.ShareableError
	if errors.As(err, &oopsErr) {
		return string(oopsErr.Code), oopsErr.Error()
	}
	return string(oops.CodeUnexpected), "allow shadow mcp inventory server"
}

func (s *Service) createShadowMCPInventoryServerAccessRule(ctx context.Context, rule accesscontrol.AccessRule) (shadowMCPInventoryServerAccessRuleSetResult, error) {
	results, err := s.accessStore.GetOrCreateRules(ctx, []accesscontrol.AccessRule{rule})
	if errors.Is(err, accesscontrol.ErrConflict) {
		existingRule, getErr := s.accessStore.GetRuleByMatch(ctx, rule.OrganizationID, rule.ResourceType, rule.AccessScope, rule.ProjectID, rule.MatchKind, rule.MatchValue)
		if getErr != nil {
			return shadowMCPInventoryServerAccessRuleSetResult{}, shadowMCPStoreErrWithConflict(ctx, s, getErr, "create shadow mcp inventory access rule", "shadow mcp access rule already exists")
		}
		if existingRule.Disposition == rule.Disposition {
			return shadowMCPInventoryServerAccessRuleSetResult{Rule: existingRule, Created: false, UpdatedFrom: nil}, nil
		}
		updatedRule, updateErr := s.updateShadowMCPInventoryServerAccessRule(ctx, rule.UpdatedBy, existingRule, rule)
		if updateErr != nil {
			return shadowMCPInventoryServerAccessRuleSetResult{}, updateErr
		}
		return shadowMCPInventoryServerAccessRuleSetResult{Rule: updatedRule, Created: false, UpdatedFrom: &existingRule}, nil
	}
	if err != nil {
		return shadowMCPInventoryServerAccessRuleSetResult{}, shadowMCPStoreErrWithConflict(ctx, s, err, "create shadow mcp inventory access rule", "shadow mcp access rule already exists")
	}
	if len(results) == 0 {
		return shadowMCPInventoryServerAccessRuleSetResult{}, oops.E(oops.CodeUnexpected, nil, "create shadow mcp inventory access rule returned no result").LogError(ctx, s.logger)
	}
	return shadowMCPInventoryServerAccessRuleSetResult{Rule: results[0].Rule, Created: results[0].Created, UpdatedFrom: nil}, nil
}

func (s *Service) updateShadowMCPInventoryServerAccessRule(ctx context.Context, userID string, existingRule, rule accesscontrol.AccessRule) (accesscontrol.AccessRule, error) {
	rule.ID = existingRule.ID
	rule.SourceRequestID = existingRule.SourceRequestID
	rule.CreatedBy = existingRule.CreatedBy
	rule.CreatedAt = existingRule.CreatedAt
	rule.UpdatedBy = userID
	rule.UpdatedAt = time.Now().UTC()

	updatedRule, err := s.accessStore.UpdateRule(ctx, rule)
	if err != nil {
		return accesscontrol.AccessRule{}, shadowMCPStoreErrWithConflict(ctx, s, err, "update shadow mcp inventory access rule", "shadow mcp access rule already exists")
	}
	return updatedRule, nil
}

func buildShadowMCPInventoryServerAccessRule(organizationID, userID, projectID string, inventoryURL shadowmcp.InventoryURL, form shadowMCPInventoryServerAccessForm) accesscontrol.AccessRule {
	now := time.Now().UTC()
	canonicalURL := inventoryURL.CanonicalURL
	urlHost := inventoryURL.URLHost
	return accesscontrol.AccessRule{
		ID:              "",
		OrganizationID:  organizationID,
		ProjectID:       projectID,
		AccessScope:     accesscontrol.AccessScopeProject,
		ResourceType:    accesscontrol.ResourceTypeShadowMCP,
		Disposition:     form.Disposition,
		MatchKind:       accesscontrol.MatchKindFullURL,
		MatchValue:      canonicalURL,
		DisplayName:     shadowMCPInventoryServerAccessDisplayName(form.ServerName, inventoryURL),
		ObservedSummary: accessControlSummaryFromShadowMCPSummary(shadowMCPSummaryFromRulePayload(&canonicalURL, &urlHost, nil)),
		SourceRequestID: "",
		CreatedBy:       userID,
		UpdatedBy:       userID,
		Reason:          conv.PtrValOr(form.Reason, ""),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func shadowMCPInventoryServerAccessDisplayName(serverName *string, inventoryURL shadowmcp.InventoryURL) string {
	if serverName != nil {
		if value := strings.TrimSpace(*serverName); value != "" {
			return value
		}
	}
	return inventoryURL.CanonicalURL
}

func (s *Service) parseShadowMCPInventoryServerAccessTarget(ctx context.Context, organizationID, rawProjectID, rawServerURL string) (uuid.UUID, shadowmcp.InventoryURL, error) {
	projectID, err := uuid.Parse(rawProjectID)
	if err != nil {
		return uuid.Nil, shadowmcp.InventoryURL{}, oops.E(oops.CodeBadRequest, err, "invalid project id").LogError(ctx, s.logger)
	}
	if err := s.requireProjectInOrganization(ctx, organizationID, projectID); err != nil {
		return uuid.Nil, shadowmcp.InventoryURL{}, err
	}

	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(rawServerURL)
	if !ok {
		return uuid.Nil, shadowmcp.InventoryURL{}, oops.E(oops.CodeBadRequest, nil, "invalid shadow mcp server url").LogError(ctx, s.logger)
	}
	return projectID, inventoryURL, nil
}

func (s *Service) buildShadowMCPInventoryAccessState(ctx context.Context, organizationID, projectID string, inventoryURL shadowmcp.InventoryURL) (*gen.ShadowMCPInventoryAccessState, error) {
	accessState, err := s.resolveShadowMCPInventoryAccessState(ctx, organizationID, projectID, inventoryURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve shadow mcp inventory access state").LogError(ctx, s.logger)
	}
	return &gen.ShadowMCPInventoryAccessState{
		CanonicalServerURL: inventoryURL.CanonicalURL,
		URLHost:            inventoryURL.URLHost,
		Access:             accessState.Access,
		Rule:               buildShadowMCPInventoryAccessRuleMatch(accessState.Rule),
	}, nil
}

func (s *Service) logShadowMCPInventoryAccessRuleCreate(ctx context.Context, organizationID, userID string, email *string, projectID uuid.UUID, reason *string, rule accesscontrol.AccessRule) error {
	ruleID, err := uuid.Parse(rule.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse shadow mcp access rule id").LogError(ctx, s.logger)
	}
	s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp inventory access rule create", func(dbtx pgx.Tx) error {
		return s.audit.LogShadowMCPAccessRuleCreate(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           organizationID,
			ProjectID:                projectID,
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, userID),
			ActorDisplayName:         email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(ruleID),
			DisplayName:              rule.DisplayName,
			MatchValue:               rule.MatchValue,
			AccessRuleSnapshotBefore: nil,
			AccessRuleSnapshotAfter:  buildShadowMCPAccessRule(rule),
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: reason},
		})
	})
	return nil
}

func (s *Service) logShadowMCPInventoryAccessRuleUpdate(ctx context.Context, organizationID, userID string, email *string, projectID uuid.UUID, reason *string, before, after accesscontrol.AccessRule) error {
	ruleID, err := uuid.Parse(after.ID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "parse shadow mcp access rule id").LogError(ctx, s.logger)
	}
	s.logShadowMCPAuditBestEffort(ctx, "log shadow mcp inventory access rule update", func(dbtx pgx.Tx) error {
		return s.audit.LogShadowMCPAccessRuleUpdate(ctx, dbtx, audit.LogShadowMCPAccessRuleEvent{
			OrganizationID:           organizationID,
			ProjectID:                projectID,
			Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, userID),
			ActorDisplayName:         email,
			ActorSlug:                nil,
			AccessRuleURN:            urn.NewShadowMCPAccessRule(ruleID),
			DisplayName:              after.DisplayName,
			MatchValue:               after.MatchValue,
			AccessRuleSnapshotBefore: buildShadowMCPAccessRule(before),
			AccessRuleSnapshotAfter:  buildShadowMCPAccessRule(after),
			Metadata:                 &audit.ShadowMCPAuditMetadata{RoleSlugs: nil, Reason: reason},
		})
	})
	return nil
}
