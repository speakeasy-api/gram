package telemetry

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

type HookInstall = repo.HookInstall
type HookInstallPage = repo.HookInstallPage

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
