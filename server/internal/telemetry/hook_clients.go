package telemetry

import (
	"context"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// HookClients reads the fleet-wide hook client grids staff use to track which
// organizations still run the legacy curl hooks and how inference hook traffic
// is spread across Claude apps. It is not scoped to an organization and runs
// no RBAC check: the admin service that exposes it authorizes the operator.
type HookClients struct {
	chRepo *repo.Queries
}

// NewHookClients builds a reader over ClickHouse telemetry.
func NewHookClients(chConn clickhouse.Conn) *HookClients {
	return &HookClients{chRepo: repo.New(chConn)}
}

type HookClientSessionRow = repo.HookClientSessionRow
type InferenceHookTrafficRow = repo.InferenceHookTrafficRow

// Sessions reads the hook client grid for the last windowDays days.
func (h *HookClients) Sessions(ctx context.Context, windowDays int) ([]HookClientSessionRow, error) {
	rows, err := h.chRepo.ListHookClientSessions(ctx, windowDays)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read hook client sessions")
	}
	return rows, nil
}

// InferenceTraffic reads the inference hook grid for the last windowDays days.
func (h *HookClients) InferenceTraffic(ctx context.Context, windowDays int) ([]InferenceHookTrafficRow, error) {
	rows, err := h.chRepo.ListInferenceHookTraffic(ctx, windowDays)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read inference hook traffic")
	}
	return rows, nil
}
