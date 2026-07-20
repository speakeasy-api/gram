package scanners

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

const (
	asyncShadowFlagGroupCacheTTL = 10 * time.Minute
	asyncShadowFlagGroupMaxSize  = 1024
)

type AsyncShadowGateReason string

const (
	AsyncShadowGateReasonSampledReal AsyncShadowGateReason = "sampled_real"
	AsyncShadowGateReasonFlagOff     AsyncShadowGateReason = "flag_off"
	AsyncShadowGateReasonGateError   AsyncShadowGateReason = "gate_error"
)

func (r AsyncShadowGateReason) Engine() string {
	if r == AsyncShadowGateReasonSampledReal {
		return AsyncScanEngineReal
	}
	return AsyncScanEngineStub
}

type AsyncShadowGate struct {
	logger *slog.Logger
	flags  feature.Provider
	db     repo.DBTX

	cache *expirable.LRU[uuid.UUID, map[string]string]
}

func NewAsyncShadowGate(logger *slog.Logger, flags feature.Provider, db repo.DBTX) *AsyncShadowGate {
	return &AsyncShadowGate{
		logger: logger,
		flags:  flags,
		db:     db,
		cache:  expirable.NewLRU[uuid.UUID, map[string]string](asyncShadowFlagGroupMaxSize, nil, asyncShadowFlagGroupCacheTTL),
	}
}

func (g *AsyncShadowGate) Decide(ctx context.Context, projectID, chatMessageID string) AsyncShadowGateReason {
	if g == nil || g.flags == nil || g.db == nil || chatMessageID == "" {
		return AsyncShadowGateReasonGateError
	}

	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		g.logger.WarnContext(ctx, "failed to parse project id for async shadow flag", attr.SlogError(err))
		return AsyncShadowGateReasonGateError
	}

	personProperties, ok := g.personProperties(ctx, parsedProjectID)
	if !ok {
		return AsyncShadowGateReasonGateError
	}

	enabled, err := g.flags.IsFlagEnabledLocal(ctx, feature.FlagRiskAsyncScanShadow, chatMessageID, nil, personProperties)
	if err != nil {
		g.logger.WarnContext(ctx, "failed to evaluate async shadow flag", attr.SlogError(err))
		return AsyncShadowGateReasonGateError
	}
	if !enabled {
		return AsyncShadowGateReasonFlagOff
	}
	return AsyncShadowGateReasonSampledReal
}

func (g *AsyncShadowGate) personProperties(ctx context.Context, projectID uuid.UUID) (map[string]string, bool) {
	if props, ok := g.cache.Get(projectID); ok {
		return props, true
	}

	row, err := repo.New(g.db).GetProjectFlagGroups(ctx, projectID)
	if err != nil {
		g.logger.WarnContext(ctx, "failed to resolve project flag groups for async shadow flag", attr.SlogError(err))
		return nil, false
	}

	props := map[string]string{
		"organization_slug": row.OrganizationSlug,
		"project_slug":      row.ProjectSlug,
	}

	g.cache.Add(projectID, props)
	return props, true
}
