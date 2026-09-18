package policyflags

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// ProjectFlagEnabled reports whether flag is on for the project's organization
// and project groups. Any lookup failure reads as off.
func ProjectFlagEnabled(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) bool {
	on, _ := ProjectFlagState(ctx, logger, queries, flags, orgID, projectID, flag)
	return on
}

// ProjectFlagState resolves the project's flag groups once and reports whether
// flag is on for them, together with the organization slug those groups
// carry so callers that need the slug for telemetry do not repeat the lookup.
// Any lookup failure reads as off with an empty slug.
func ProjectFlagState(ctx context.Context, logger *slog.Logger, queries *repo.Queries, flags feature.Provider, orgID string, projectID uuid.UUID, flag feature.Flag) (enabled bool, orgSlug string) {
	if flags == nil {
		return false, ""
	}
	groups, err := queries.GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		logger.WarnContext(ctx, "resolve project flag groups failed", attr.SlogError(err), attr.SlogOrganizationID(orgID), attr.SlogProjectID(projectID.String()))
		return false, ""
	}
	on, err := flags.IsFlagEnabled(ctx, flag, orgID, feature.OrgProjectGroups(groups.OrganizationSlug, groups.ProjectSlug))
	if err != nil {
		logger.WarnContext(ctx, "project flag check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return false, ""
	}
	return on, groups.OrganizationSlug
}
