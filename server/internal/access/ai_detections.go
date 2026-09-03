package access

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	"github.com/speakeasy-api/gram/server/internal/oops"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// ListAIDetections aggregates device-agent AI scan detections per target for
// the caller's organization. Display names and categories are decorated from
// the aitargets catalog at read time; target ids the catalog does not know —
// agent binaries can ship newer target lists than the catalog — are echoed
// under their raw id with the category recorded at detection time.
//
// Org-scoped by design: detections attach to devices and enrolled users, not
// projects (the same shape as agent.listSyncedUsers).
func (s *Service) ListAIDetections(ctx context.Context, payload *gen.ListAIDetectionsPayload) (*gen.ListAIDetectionsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if contextvalues.IsSupportSession(ctx) {
		if err := s.authz.Require(ctx, authz.Check{
			Scope:        authz.ScopeOrgAdmin,
			ResourceKind: "",
			ResourceID:   ac.ActiveOrganizationID,
			Dimensions:   nil,
		}); err != nil {
			return nil, err
		}
	} else if err := s.authz.RequireUserOrganizationScope(ctx, ac.ActiveOrganizationID, ac.UserID, authz.ScopeOrgAdmin); err != nil {
		return nil, fmt.Errorf("authorize AI detections organization administrator: %w", err)
	}

	var categories []string
	if payload.Category != nil {
		categories = []string{*payload.Category}
	}

	// The team filter resolves a SCIM directory group to its active members'
	// normalized emails and pushes them down to ClickHouse as a user_email
	// restriction. A group with no active members matches nothing.
	var userEmails []string
	if payload.DirectoryGroupID != nil {
		groupID, err := uuid.Parse(*payload.DirectoryGroupID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid directory group id").LogError(ctx, s.logger)
		}
		emails, err := directory.NewService(s.db).ListActiveGroupMemberEmails(ctx, ac.ActiveOrganizationID, groupID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve directory group members").LogError(ctx, s.logger)
		}
		if len(emails) == 0 {
			return &gen.ListAIDetectionsResult{Detections: []*gen.AIDetection{}}, nil
		}
		userEmails = emails
	}

	return s.listAIDetectionModels(ctx, telemetryrepo.ListAIDetectionSummariesParams{
		OrganizationID:       ac.ActiveOrganizationID,
		Categories:           categories,
		UserEmails:           userEmails,
		ExactUserEmail:       "",
		CanonicalIdentityOrg: s.canonicalFoldOrg(ctx, ac.ActiveOrganizationID),
	})
}

// ListEmployeeAIDetections returns one employee's organization-scoped device
// detections to callers who can read the active project. The required email
// keeps this lower-privilege endpoint from becoming an organization-wide list.
func (s *Service) ListEmployeeAIDetections(ctx context.Context, payload *gen.ListEmployeeAIDetectionsPayload) (*gen.ListAIDetectionsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil || ac.ProjectID == nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing project auth context").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        authz.ScopeProjectRead,
		ResourceKind: "",
		ResourceID:   ac.ProjectID.String(),
		Dimensions:   nil,
	}); err != nil {
		return nil, err
	}
	if err := s.requireProjectInOrganization(ctx, ac.ActiveOrganizationID, *ac.ProjectID); err != nil {
		return nil, err
	}

	userEmail := conv.NormalizeEmail(strings.TrimSpace(payload.UserEmail))
	if userEmail == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "employee email is required").LogError(ctx, s.logger)
	}

	return s.listAIDetectionModels(ctx, telemetryrepo.ListAIDetectionSummariesParams{
		OrganizationID:       ac.ActiveOrganizationID,
		Categories:           nil,
		UserEmails:           nil,
		ExactUserEmail:       userEmail,
		CanonicalIdentityOrg: s.canonicalFoldOrg(ctx, ac.ActiveOrganizationID),
	})
}

func (s *Service) listAIDetectionModels(ctx context.Context, params telemetryrepo.ListAIDetectionSummariesParams) (*gen.ListAIDetectionsResult, error) {
	rows, err := telemetryrepo.New(s.chConn).ListAIDetectionSummaries(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ai detections").LogError(ctx, s.logger)
	}

	detections := make([]*gen.AIDetection, 0, len(rows))
	for _, row := range rows {
		displayName := row.TargetID
		category := row.Category
		if target, known := aitargets.ByID(row.TargetID); known {
			displayName = target.DisplayName
			category = string(target.Category)
		}
		detections = append(detections, &gen.AIDetection{
			TargetID:    row.TargetID,
			DisplayName: displayName,
			Category:    category,
			UserCount:   int64(row.UserCount),   //nolint:gosec // distinct enrolled users cannot approach int64 overflow
			DeviceCount: int64(row.DeviceCount), //nolint:gosec // distinct devices cannot approach int64 overflow
			Signals:     row.Signals,
			Versions:    row.Versions,
			FirstSeen:   formatTimeValue(row.FirstSeen),
			LastSeen:    formatTimeValue(row.LastSeen),
		})
	}

	return &gen.ListAIDetectionsResult{Detections: detections}, nil
}
