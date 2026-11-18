//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
)

const organizationAccessSideEffects = "Disabling blocks organisation access according to the dashboard's existing behaviour. It does not cancel subscriptions, change provider keys or immediately terminate in-flight work."

// OrganizationAccessInput identifies one exact organisation.
type OrganizationAccessInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
}

type organizationAccessReceipt struct {
	OrganizationID string     `json:"organization_id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	EnabledBefore  bool       `json:"enabled_before"`
	EnabledAfter   bool       `json:"enabled_after"`
	DisabledAt     *time.Time `json:"disabled_at"`
	SideEffects    string     `json:"side_effects"`
}

// organizationAccessWriter handles exactly one of the existing dashboard
// operations.
type organizationAccessWriter struct {
	runner    *writeRunner
	audit     *audit.Logger
	writes    WriteConfig
	operation WriteOperation
}

func (w *organizationAccessWriter) desiredEnabled() bool {
	return w.operation == OperationEnableOrganization
}

func (w *organizationAccessWriter) apply(ctx context.Context, input OrganizationAccessInput) (organizationAccessReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, w.operation)
	if err != nil {
		return organizationAccessReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return organizationAccessReceipt{}, err
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (organizationAccessReceipt, error) {
		before, err := admin.LockOrganizationAccessTx(ctx, tx, input.OrganizationID)
		if errors.Is(err, admin.ErrOrganizationAccessNotFound) {
			return organizationAccessReceipt{}, ErrStaleState
		}
		if err != nil {
			return organizationAccessReceipt{}, fmt.Errorf("read organization access state: %w", err)
		}
		enabledBefore := before.DisabledAt == nil
		if enabledBefore == w.desiredEnabled() {
			return organizationAccessReceipt{}, errors.New("organization already has the requested access state; nothing would change")
		}
		ctx, actor, displayName := staffMutation(ctx, authority)
		after, err := admin.SetOrganizationAccessTx(ctx, tx, w.audit, input.OrganizationID, w.desiredEnabled(), actor, displayName)
		if errors.Is(err, admin.ErrOrganizationAccessNotFound) {
			return organizationAccessReceipt{}, ErrStaleState
		}
		if err != nil {
			return organizationAccessReceipt{}, fmt.Errorf("set organization access: %w", err)
		}
		return organizationAccessReceipt{OrganizationID: after.OrganizationID, Name: after.Name, Slug: after.Slug, EnabledBefore: enabledBefore, EnabledAfter: w.desiredEnabled(), DisabledAt: after.DisabledAt, SideEffects: organizationAccessSideEffects}, nil
	})
}

func (w *organizationAccessWriter) register(server *mcp.Server) {
	name := string(OperationEnableOrganization)
	title := "Enable Organization"
	description := "Enable access for one exact canonical organization ID."
	if !w.desiredEnabled() {
		name = string(OperationDisableOrganization)
		title = "Disable Organization"
		description = "Disable access for one exact canonical organization ID. Disabling blocks access according to existing dashboard behaviour, but does not cancel subscriptions, change provider keys or immediately terminate in-flight work."
	}
	addWriteTool(server, &mcp.Tool{Name: name, Title: title, Description: description}, w.apply)
}
