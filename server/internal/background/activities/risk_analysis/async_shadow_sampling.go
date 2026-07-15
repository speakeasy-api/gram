package risk_analysis

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type asyncShadowPayload struct {
	SampleRate float64 `json:"sample_rate"`
}

func (a *AnalyzeBatch) asyncShadowSampleRate(ctx context.Context, orgID string, projectID uuid.UUID) float64 {
	if a.flags == nil {
		return 0
	}

	groups, err := repo.New(a.db).GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		a.logger.WarnContext(ctx, "resolve async shadow flag groups failed", attr.SlogError(err), attr.SlogOrganizationID(orgID), attr.SlogProjectID(projectID.String()))
		return 0
	}

	flagGroups := feature.OrgProjectGroups(groups.OrganizationSlug, groups.ProjectSlug)
	on, err := a.flags.IsFlagEnabled(ctx, feature.FlagRiskAsyncScanShadow, orgID, flagGroups)
	if err != nil {
		a.logger.WarnContext(ctx, "async shadow flag check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return 0
	}
	if !on {
		return 0
	}

	payload, err := a.flags.FlagPayload(ctx, feature.FlagRiskAsyncScanShadow, orgID, flagGroups)
	if err != nil {
		a.logger.WarnContext(ctx, "async shadow flag payload check failed", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return 0
	}
	var parsed asyncShadowPayload
	if len(payload) == 0 || json.Unmarshal(payload, &parsed) != nil || parsed.SampleRate < 0 || parsed.SampleRate > 1 {
		return 0
	}
	return parsed.SampleRate
}

func sampleAsyncShadow(chatMessageID string, sampleRate float64) bool {
	if sampleRate <= 0 {
		return false
	}
	if sampleRate >= 1 {
		return true
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(chatMessageID))
	unit := math.Ldexp(float64(h.Sum64()>>11), -53)
	return unit < sampleRate
}
