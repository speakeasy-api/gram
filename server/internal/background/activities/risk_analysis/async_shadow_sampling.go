package risk_analysis

import (
	"context"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func (a *AnalyzeBatch) asyncShadowPersonProperties(ctx context.Context, orgID string, projectID uuid.UUID) (map[string]string, bool) {
	if a.flags == nil {
		return nil, false
	}

	groups, err := repo.New(a.db).GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		a.logger.WarnContext(ctx, "resolve async shadow flag properties failed",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
			attr.SlogProjectID(projectID.String()),
		)
		return nil, false
	}

	return map[string]string{
		"organization_slug": groups.OrganizationSlug,
		"project_slug":      groups.ProjectSlug,
	}, true
}

func (a *AnalyzeBatch) asyncShadowEnabled(ctx context.Context, orgID string, projectID uuid.UUID, chatMessageID string, personProperties map[string]string) bool {
	if a.flags == nil {
		return false
	}

	on, err := a.flags.IsFlagEnabledLocal(ctx, feature.FlagRiskAsyncScanShadow, chatMessageID, nil, personProperties)
	if err != nil {
		a.logger.ErrorContext(ctx, "async shadow flag local evaluation failed",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
			attr.SlogProjectID(projectID.String()),
			attr.SlogMessageID(chatMessageID),
			attr.SlogValueString(string(feature.FlagRiskAsyncScanShadow)),
		)
		return false
	}
	return on
}
