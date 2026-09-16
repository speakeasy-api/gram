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
	if err := s.requireLiveOrgAdmin(ctx, ac); err != nil {
		return nil, err
	}

	var category string
	if payload.Category != nil {
		category = *payload.Category
	}

	// The team filter resolves a SCIM directory group to its active members'
	// normalized emails and pushes them down to ClickHouse as a user_email
	// restriction. A group with no active members matches nothing.
	//
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

	result, err := s.listAIDetectionModels(ctx, telemetryrepo.ListAIDetectionSummariesParams{
		OrganizationID:       ac.ActiveOrganizationID,
		Categories:           nil,
		UserEmails:           userEmails,
		ExactUserEmail:       "",
		CanonicalIdentityOrg: s.canonicalFoldOrg(ctx, ac.ActiveOrganizationID),
	})
	if err != nil {
		return nil, err
	}
	return keepDetectionCategory(result, category), nil
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

	result, err := s.listAIDetectionModels(ctx, telemetryrepo.ListAIDetectionSummariesParams{
		OrganizationID:       ac.ActiveOrganizationID,
		Categories:           nil,
		UserEmails:           nil,
		ExactUserEmail:       userEmail,
		CanonicalIdentityOrg: s.canonicalFoldOrg(ctx, ac.ActiveOrganizationID),
	})
	if err != nil {
		return nil, err
	}
	// The counts are about the employee the caller already named, so they say
	// nothing new. Why an administrator decided about a tool is a different
	// matter, and not part of what this endpoint answers.
	redactAIDecisionRationale(result.Detections)
	return result, nil
}

func (s *Service) listAIDetectionModels(ctx context.Context, params telemetryrepo.ListAIDetectionSummariesParams) (*gen.ListAIDetectionsResult, error) {
	rows, err := telemetryrepo.New(s.chConn).ListAIDetectionSummaries(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ai detections").LogError(ctx, s.logger)
	}

	// Trouble reading the organization's scan targets degrades to the stored
	// ids and categories.
	queries := agentrepo.New(s.db)
	// The target and what the organization decided about it come off one row,
	// so one read covers both. Trouble reading it degrades rather than fails:
	// tools list under their stored ids and read unreviewed. Nothing is
	// enforced from here, so a degraded status column beats no inventory.
	var catalog *aitargets.OrganizationList
	if list, err := aitargets.LoadOrganizationList(ctx, queries, params.OrganizationID); err != nil {
		s.logger.WarnContext(ctx, "ai scan targets unavailable; listing detections as stored", attr.SlogError(err))
		catalog = &aitargets.OrganizationList{Entries: nil, Snapshot: aitargets.NewSnapshot(0, nil)}
	} else {
		catalog = list
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
		decision := aitargets.UnreviewedDecisionRecord(row.TargetID)
		if entry, ok := catalog.Entry(row.TargetID); ok {
			target = entry.Target
			decision = entry.Decision
			displayName = entry.DisplayName
			category = string(entry.Category)
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
			Access:      aiToolAccessView(aitargets.SummarizeAccess(target, decision)),
		})
	}

	return &gen.ListAIDetectionsResult{Detections: detections}, nil
}

// keepDetectionCategory narrows a decorated result to one category, or leaves
// it whole when category is empty.
//
// Filtered here, after decoration, rather than pushed down to ClickHouse on
// purpose. A detection row stores the category the catalog gave its target at
// the time it was written, while the response reports the catalog's current
// category, so the two diverge whenever a target is reclassified: a stored
// "harness" row now reads "assistant". Filtering on the stored value would
// then omit that tool from the category it now belongs to and return it under
// the one it left. The effective category is the one the caller sees, so it
// is the one filtered on. The summaries query aggregates the whole
// organization with no paging, so nothing is lost by filtering afterwards.
func keepDetectionCategory(result *gen.ListAIDetectionsResult, category string) *gen.ListAIDetectionsResult {
	if category == "" || result == nil {
		return result
	}
	kept := make([]*gen.AIDetection, 0, len(result.Detections))
	for _, detection := range result.Detections {
		if detection != nil && detection.Category == category {
			kept = append(kept, detection)
		}
	}
	result.Detections = kept
	return result
}

// AIDetectionsReadInput is an organization-scoped Shadow AI inventory read for
// a trusted internal caller that has established its own principal.
type AIDetectionsReadInput struct {
	OrganizationID string

	// Category narrows to harness, assistant or local_model; empty is all.
	Category string
}

// ReadAIDetections is the seam the Platform MCP reads through, mirroring
// ReadShadowMCPInventory for the MCP half of the same section.
//
// Returns the full row. Every caller of this section is an organization
// administrator; the one endpoint that is not, listEmployeeAIDetections, does
// not read through here.
func (s *Service) ReadAIDetections(ctx context.Context, input AIDetectionsReadInput) (*gen.ListAIDetectionsResult, error) {
	if strings.TrimSpace(input.OrganizationID) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "organization id is required").LogError(ctx, s.logger)
	}

	category := strings.TrimSpace(input.Category)
	if category != "" && !slices.Contains(aitargets.KnownCategories(), aitargets.Category(category)) {
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown detection category %q", category).LogError(ctx, s.logger)
	}

	result, err := s.listAIDetectionModels(ctx, telemetryrepo.ListAIDetectionSummariesParams{
		OrganizationID:       input.OrganizationID,
		Categories:           nil,
		UserEmails:           nil,
		ExactUserEmail:       "",
		CanonicalIdentityOrg: s.canonicalFoldOrg(ctx, input.OrganizationID),
	})
	if err != nil {
		return nil, err
	}
	return keepDetectionCategory(result, category), nil
}
