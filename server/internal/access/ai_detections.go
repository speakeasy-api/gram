package access

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
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
// The reads are org-scoped: detections attach to devices and enrolled users,
// not projects (the same shape as agent.listSyncedUsers). The surface is
// nonetheless reached through a project, which is why project:read admits a
// caller — see inventoryProjection for what that scope does and does not see.
func (s *Service) ListAIDetections(ctx context.Context, payload *gen.ListAIDetectionsPayload) (*gen.ListAIDetectionsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	projection, err := s.resolveAIInventoryProjection(ctx, ac, conv.PtrValOr(ac.ProjectID, uuid.Nil))
	if err != nil {
		return nil, err
	}

	var categories []string
	if payload.Category != nil {
		categories = []string{*payload.Category}
	}

	// The team filter resolves a SCIM directory group to its active members'
	// normalized emails and pushes them down to ClickHouse as a user_email
	// restriction. A group with no active members matches nothing.
	//
	// It is attribution, not a convenience: narrowing an organization-wide
	// inventory to one team and reading the result off tells you what that
	// team runs, which is exactly what the unattributed projection withholds.
	var userEmails []string
	if payload.DirectoryGroupID != nil {
		if !projection.Attributed {
			return nil, oops.E(oops.CodeForbidden, nil, "filtering AI detections by team requires organization administrator access").LogError(ctx, s.logger)
		}
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

	result, err := s.listAIDetectionModels(ctx, telemetryrepo.ListAIDetectionSummariesParams{
		OrganizationID:       ac.ActiveOrganizationID,
		Categories:           categories,
		UserEmails:           userEmails,
		ExactUserEmail:       "",
		CanonicalIdentityOrg: s.canonicalFoldOrg(ctx, ac.ActiveOrganizationID),
	})
	if err != nil {
		return nil, err
	}
	if !projection.Attributed {
		redactAIDetectionAttribution(result.Detections)
	}
	return result, nil
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

	// Trouble reading the organization's scan targets degrades to the stored
	// ids and categories.
	queries := agentrepo.New(s.db)
	catalog := aitargets.NewSnapshot(0, nil)
	if list, err := aitargets.LoadOrganizationList(ctx, queries, params.OrganizationID); err != nil {
		s.logger.WarnContext(ctx, "ai scan targets unavailable; listing detections as stored", attr.SlogError(err))
	} else {
		catalog = list.Snapshot
	}

	// Trouble reading decisions degrades the same way: every tool reads as
	// unreviewed rather than the list failing. Nothing is enforced from this
	// read, so a degraded status column is strictly better than no inventory.
	decisions, err := aitargets.LoadDecisions(ctx, queries, params.OrganizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "ai tool decisions unavailable; listing detections as unreviewed", attr.SlogError(err))
		decisions = nil
	}

	detections := make([]*gen.AIDetection, 0, len(rows))
	for _, row := range rows {
		displayName := row.TargetID
		category := row.Category
		// A detection can name a target the catalog no longer serves, because
		// an agent binary can ship a newer list than the server's. The zero
		// target is the honest input for the access summary there: nothing
		// matches a caller to it any more, so nothing is enforceable.
		target := aitargets.ZeroTarget()
		if known, ok := catalog.ByID(row.TargetID); ok {
			target = known
			displayName = known.DisplayName
			category = string(known.Category)
		}
		detections = append(detections, &gen.AIDetection{
			TargetID:    row.TargetID,
			DisplayName: displayName,
			Category:    category,
			// Attribution: always built, then dropped for callers without
			// org:admin by redactAIDetectionAttribution.
			UserCount:   new(int64(row.UserCount)),   //nolint:gosec // distinct enrolled users cannot approach int64 overflow
			DeviceCount: new(int64(row.DeviceCount)), //nolint:gosec // distinct devices cannot approach int64 overflow
			Signals:     row.Signals,
			Versions:    row.Versions,
			FirstSeen:   formatTimeValue(row.FirstSeen),
			LastSeen:    formatTimeValue(row.LastSeen),
			Access:      aiToolAccessView(aitargets.SummarizeAccess(target, aitargets.Decisions(decisions, row.TargetID)), true),
		})
	}

	return &gen.ListAIDetectionsResult{Detections: detections}, nil
}

// AIDetectionsReadInput is an organization-scoped Shadow AI inventory read for
// a trusted internal caller that has established its own principal.
type AIDetectionsReadInput struct {
	OrganizationID string

	// Category narrows to harness, assistant or local_model; empty is all.
	Category string

	// Attributed permits the user and device counts through. The caller
	// decides it, having resolved a projection this function cannot see.
	Attributed bool
}

// ReadAIDetections is the seam the Platform MCP reads through, mirroring
// ReadShadowMCPInventory for the MCP half of the same section.
func (s *Service) ReadAIDetections(ctx context.Context, input AIDetectionsReadInput) (*gen.ListAIDetectionsResult, error) {
	if strings.TrimSpace(input.OrganizationID) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "organization id is required").LogError(ctx, s.logger)
	}

	var categories []string
	if category := strings.TrimSpace(input.Category); category != "" {
		if !slices.Contains(aitargets.KnownCategories(), aitargets.Category(category)) {
			return nil, oops.E(oops.CodeBadRequest, nil, "unknown detection category %q", category).LogError(ctx, s.logger)
		}
		categories = []string{category}
	}

	result, err := s.listAIDetectionModels(ctx, telemetryrepo.ListAIDetectionSummariesParams{
		OrganizationID:       input.OrganizationID,
		Categories:           categories,
		UserEmails:           nil,
		ExactUserEmail:       "",
		CanonicalIdentityOrg: s.canonicalFoldOrg(ctx, input.OrganizationID),
	})
	if err != nil {
		return nil, err
	}
	if !input.Attributed {
		redactAIDetectionAttribution(result.Detections)
	}
	return result, nil
}
