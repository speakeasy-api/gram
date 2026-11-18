package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type HookInstall = repo.HookInstall
type HookInstallPage = repo.HookInstallPage
type HookVersionMixRow = repo.HookVersionMixRow

// HookFleet reads one organization's hooks installs for staff. It runs no
// RBAC check: the admin service that exposes it authorizes the operator.
type HookFleet struct {
	chRepo *repo.Queries
}

func NewHookFleet(chConn clickhouse.Conn) *HookFleet {
	return &HookFleet{chRepo: repo.New(chConn)}
}

func (h *HookFleet) HookInstalls(ctx context.Context, organizationID string, since time.Time, limit int) (HookInstallPage, error) {
	if organizationID == "" {
		return HookInstallPage{}, oops.E(oops.CodeInvalid, nil, "organization id is required")
	}
	page, err := h.chRepo.ListHookInstalls(ctx, organizationID, since, limit)
	if err != nil {
		return HookInstallPage{}, oops.E(oops.CodeUnexpected, err, "failed to read hook installs")
	}
	return page, nil
}

// VersionMix counts installs per organization, version and endpoint
// generation over the UTC days in [from, to).
func (h *HookFleet) VersionMix(ctx context.Context, from, to time.Time) ([]HookVersionMixRow, error) {
	rows, err := h.chRepo.ListHookVersionMix(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("read hook version mix: %w", err)
	}
	return rows, nil
}
