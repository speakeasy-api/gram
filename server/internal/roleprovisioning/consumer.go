package roleprovisioning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pluginsv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/hints"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/repo"
	"github.com/speakeasy-api/gram/server/internal/streams"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const maintenancePageSize = 100

type Reconciler interface {
	Reconcile(context.Context, string, string, Actor) (Result, error)
}

// Consumer reloads current state; durable requests never contain mutation intent.
type Consumer struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	service Reconciler
}

func NewConsumer(logger *slog.Logger, db *pgxpool.Pool, service Reconciler) *Consumer {
	return &Consumer{logger: logger, db: db, service: service}
}

func (h *Consumer) HandleBatchWithResult(ctx context.Context, messages []streams.BatchMessage[*pluginsv1.RoleProvisioningRequested]) error {
	for _, message := range messages {
		if err := h.HandlePage(ctx, message.Message); err != nil {
			message.Fail(err)
		}
	}
	return nil
}

// HandlePage completes at most one 100-row page. A crash before the continuation
// commit replays the page; Reconcile owns short, independently idempotent txs.
func (h *Consumer) HandlePage(ctx context.Context, request *pluginsv1.RoleProvisioningRequested) error {
	org := request.GetOrganizationId()
	global := request.GetGlobalRoleUrn() != "" || request.GetGlobalSweep()
	if (request.GetGlobalSweep() && request.GetGlobalRoleUrn() != "") || (org == "") != global || (global && (request.GetRoleUrn() != "" || request.GetPluginId() != "" || request.GetAfterRoleUrn() != "")) || (!global && request.GetAfterOrganizationId() != "") || (request.GetRoleUrn() != "" && request.GetAfterRoleUrn() != "") {
		h.logger.WarnContext(ctx, "invalid role provisioning scope")
		return errors.New("invalid role provisioning scope")
	}
	if request.GetGlobalRoleUrn() != "" && !validRoleURN(request.GetGlobalRoleUrn(), "global") {
		return errors.New("invalid global role provisioning identifier")
	}
	if request.GetRoleUrn() != "" && !validRoleURN(request.GetRoleUrn(), "") {
		return errors.New("invalid role provisioning identifier")
	}
	if request.GetAfterRoleUrn() != "" && !validRoleURN(request.GetAfterRoleUrn(), "") {
		return errors.New("invalid role provisioning cursor")
	}
	pluginID := uuid.Nil
	if request.GetPluginId() != "" {
		var err error
		pluginID, err = uuid.Parse(request.GetPluginId())
		if err != nil || pluginID == uuid.Nil {
			return errors.New("invalid role provisioning plugin identifier")
		}
	}
	if global {
		return h.fanout(ctx, request)
	}
	page, err := repo.New(h.db).ListMaintenanceRoles(ctx, repo.ListMaintenanceRolesParams{
		OrganizationID: org, AfterRoleUrn: request.GetAfterRoleUrn(), RoleUrn: request.GetRoleUrn(), PluginID: pluginID, PageSize: maintenancePageSize,
	})
	if err != nil {
		return fmt.Errorf("list maintenance roles: %w", err)
	}
	// A role-scoped message owns its transport retry/DLQ lifecycle. Never turn
	// a failed retry into another fresh outbox message (which would reset retries).
	// Traversal pages instead durably isolate failures before acknowledging, so a
	// persistent failure cannot fork another continuation chain on every delivery.
	var pending []hints.Hint
	for _, role := range page {
		if !validRoleURN(role, "") {
			continue
		}
		_, err := h.service.Reconcile(ctx, org, role, Actor{Principal: urn.NewSystemPrincipal("role-provisioning"), PublicationUserID: ""})
		if err != nil {
			if request.GetRoleUrn() != "" {
				return fmt.Errorf("reconcile maintenance role: %w", err)
			}
			pending = append(pending, hints.Hint{OrganizationID: org, RoleURN: role, PluginID: uuid.Nil, AfterRoleURN: "", GlobalRoleURN: "", GlobalSweep: false, AfterOrganizationID: ""})
		}
	}
	if len(page) == maintenancePageSize {
		pending = append(pending, hints.Hint{OrganizationID: org, RoleURN: request.GetRoleUrn(), PluginID: pluginID, AfterRoleURN: page[len(page)-1], GlobalRoleURN: "", GlobalSweep: false, AfterOrganizationID: ""})
	}
	if len(pending) == 0 {
		return nil
	}
	// Retry hints and the continuation commit together. A crash before commit
	// replays the source; a crash after commit may duplicate this bounded work,
	// but role retry failures never spawn new messages or repeat traversal.
	return h.enqueue(ctx, pending...)

}

func validRoleURN(value, scope string) bool {
	for _, prefix := range []string{"role:organization:", "role:global:"} {
		if scope != "" && prefix != "role:"+scope+":" {
			continue
		}
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		id, err := uuid.Parse(strings.TrimPrefix(value, prefix))
		return err == nil && id != uuid.Nil && value == prefix+id.String()
	}
	return false
}

func (h *Consumer) enqueue(ctx context.Context, pending ...hints.Hint) error {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin maintenance continuation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, hint := range pending {
		if err := hints.Emit(ctx, tx, hint); err != nil {
			return fmt.Errorf("enqueue maintenance continuation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit maintenance continuation: %w", err)
	}
	return nil
}

func (h *Consumer) fanout(ctx context.Context, request *pluginsv1.RoleProvisioningRequested) error {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin maintenance organization fanout: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	orgs, err := repo.New(tx).ListMaintenanceOrganizations(ctx, repo.ListMaintenanceOrganizationsParams{
		AfterOrganizationID: request.GetAfterOrganizationId(), GlobalRoleUrn: request.GetGlobalRoleUrn(), PageSize: maintenancePageSize,
	})
	if err != nil {
		return fmt.Errorf("list maintenance organizations: %w", err)
	}
	for _, org := range orgs {
		if err := hints.Emit(ctx, tx, hints.Hint{OrganizationID: org, RoleURN: request.GetGlobalRoleUrn(), PluginID: uuid.Nil, GlobalRoleURN: "", GlobalSweep: false, AfterOrganizationID: "", AfterRoleURN: ""}); err != nil {
			return fmt.Errorf("enqueue organization maintenance: %w", err)
		}
	}
	if len(orgs) == maintenancePageSize {
		if err := hints.Emit(ctx, tx, hints.Hint{GlobalRoleURN: request.GetGlobalRoleUrn(), GlobalSweep: request.GetGlobalSweep(), AfterOrganizationID: orgs[len(orgs)-1], OrganizationID: "", RoleURN: "", PluginID: uuid.Nil, AfterRoleURN: ""}); err != nil {
			return fmt.Errorf("enqueue maintenance fanout continuation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit maintenance organization fanout: %w", err)
	}
	return nil
}
