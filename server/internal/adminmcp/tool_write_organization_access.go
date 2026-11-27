//nolint:exhaustruct_v5 // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
)

const organizationAccessSideEffects = "Disabling blocks organisation access according to the dashboard's existing behaviour and locks the organisation's platform inference keys; enabling releases that lock. It does not cancel subscriptions, delete provider keys or immediately terminate in-flight work."

// organizationKeySyncTimeout bounds the inference key work that follows an
// access change: two key types, each waiting for its billing lock and making
// one OpenRouter call.
const organizationKeySyncTimeout = 30 * time.Second

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

	// InferenceKeys reports whether the platform inference keys now match the
	// access state: "synced", "not configured", or "failed" with the access
	// change still applied.
	InferenceKeys string `json:"inference_keys"`
}

const (
	inferenceKeysSynced        = "synced"
	inferenceKeysNotConfigured = "not configured"
	inferenceKeysFailed        = "failed: the access change is applied, but the inference keys could not be updated; check the organization's billing page"
)

// organizationAccessWriter handles exactly one of the existing dashboard
// operations.
type organizationAccessWriter struct {
	runner    *writeRunner
	audit     *audit.Logger
	writes    WriteConfig
	operation WriteOperation

	// keys locks and releases the platform inference keys after the access
	// change commits. Nil leaves the keys alone.
	keys admin.OrganizationInferenceKeys

	// logger records a key sync that failed after the access change committed.
	logger *slog.Logger
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
	receipt, err := runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (organizationAccessReceipt, error) {
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
		return organizationAccessReceipt{OrganizationID: after.OrganizationID, Name: after.Name, Slug: after.Slug, EnabledBefore: enabledBefore, EnabledAfter: w.desiredEnabled(), DisabledAt: after.DisabledAt, SideEffects: organizationAccessSideEffects, InferenceKeys: inferenceKeysNotConfigured}, nil
	})
	if err != nil {
		return receipt, err
	}
	if w.keys == nil {
		return receipt, nil
	}

	// The access change has committed and stands whatever happens to the keys.
	keyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), organizationKeySyncTimeout)
	defer cancel()
	err = admin.SyncInferenceKeysWithOrganizationAccess(keyCtx, w.keys, receipt.OrganizationID, w.desiredEnabled())
	switch {
	case err == nil:
		receipt.InferenceKeys = inferenceKeysSynced
	case errors.Is(err, admin.ErrOpenRouterUnavailable):
	default:
		receipt.InferenceKeys = inferenceKeysFailed
		w.logger.ErrorContext(ctx, "sync inference keys after organization access change", attr.SlogError(err), attr.SlogOrganizationID(receipt.OrganizationID))
	}
	return receipt, nil
}

func (w *organizationAccessWriter) register(server *mcp.Server) {
	name := string(OperationEnableOrganization)
	title := "Enable Organization"
	description := "Enable access for one exact canonical organization ID."
	if !w.desiredEnabled() {
		name = string(OperationDisableOrganization)
		title = "Disable Organization"
		description = "Disable access for one exact canonical organization ID. Disabling blocks access according to existing dashboard behaviour and locks the organisation's platform inference keys until it is enabled again. It does not cancel subscriptions, delete provider keys or immediately terminate in-flight work."
	}
	addWriteTool(server, &mcp.Tool{Name: name, Title: title, Description: description}, w.apply)
}
