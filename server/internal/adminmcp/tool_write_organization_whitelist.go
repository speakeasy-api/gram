//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

const organizationWhitelistSideEffects = "Changes the dashboard's demo-access gate when session information is refreshed. Removing whitelisting may show the demo gate or existing trial-ended screen; it is not credential revocation and does not stop in-flight work. Whitelisting suppresses base-tier access-paused billing notifications. Does not change account type, subscriptions, trial records, provider keys or disabled state. Organisations with any trial record, regardless of tier or lifecycle state, are unavailable for this MCP operation. Existing signup, billing and trial lifecycle flows may change whitelisting independently. Records a tenant audit event."

var errWhitelistTrial = errors.New("organisations with a trial record are unavailable for this whitelist operation; use the staff dashboard")

// OrganizationWhitelistInput describes one exact demo-gate change.
type OrganizationWhitelistInput struct {
	// OrganizationID is a canonical ID, never a name or slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`

	// Whitelisted is the desired demo-gate setting.
	Whitelisted bool `json:"whitelisted" jsonschema:"Whether to bypass the dashboard demo-access gate; does not enable a disabled organization or revoke credentials"`
}

type organizationWhitelistReceipt struct {
	Target      admin.OrganizationWhitelistState `json:"target"`
	After       bool                             `json:"whitelisted_after"`
	SideEffects string                           `json:"side_effects"`
}

type organizationWhitelistWriter struct {
	runner *writeRunner
	audit  *audit.Logger
	writes WriteConfig
}

func (w *organizationWhitelistWriter) readState(ctx context.Context, tx pgx.Tx, organizationID string) (admin.OrganizationWhitelistState, error) {
	state, err := admin.LockOrganizationWhitelistTx(ctx, tx, organizationID)
	if errors.Is(err, admin.ErrOrganizationWhitelistNotFound) {
		return admin.OrganizationWhitelistState{}, ErrStaleState
	}
	if err != nil {
		return admin.OrganizationWhitelistState{}, fmt.Errorf("read organization whitelist state: %w", err)
	}
	// Read after the metadata lock: trial creation takes that same row lock.
	// A non-locking read avoids reversing conversion's trial-to-organization lock order.
	_, err = trialsrepo.New(tx).GetTrial(ctx, organizationID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return admin.OrganizationWhitelistState{}, fmt.Errorf("check trial before whitelist change: %w", err)
	}
	if err == nil {
		return admin.OrganizationWhitelistState{}, errWhitelistTrial
	}
	return state, nil
}

func (w *organizationWhitelistWriter) apply(ctx context.Context, input OrganizationWhitelistInput) (organizationWhitelistReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationSetOrganizationWhitelist)
	if err != nil {
		return organizationWhitelistReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return organizationWhitelistReceipt{}, err
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (organizationWhitelistReceipt, error) {
		state, err := w.readState(ctx, tx, input.OrganizationID)
		if err != nil {
			return organizationWhitelistReceipt{}, err
		}
		if state.Whitelisted == input.Whitelisted {
			return organizationWhitelistReceipt{}, errors.New("organization already has the requested whitelist setting; nothing would change")
		}
		ctx, actor, displayName := staffMutation(ctx, authority)
		after, err := admin.SetOrganizationWhitelistTx(ctx, tx, w.audit, input.OrganizationID, input.Whitelisted, actor, displayName)
		if err != nil {
			return organizationWhitelistReceipt{}, fmt.Errorf("set organization whitelist: %w", err)
		}
		return organizationWhitelistReceipt{Target: state, After: after, SideEffects: organizationWhitelistSideEffects}, nil
	})
}

func (w *organizationWhitelistWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationSetOrganizationWhitelist), Title: "Set Organization Whitelist", Description: "Change the dashboard demo-access whitelist for one exact canonical organization ID without any trial record, regardless of tier or lifecycle state. Does not change account type, subscriptions, trial records or disabled state, revoke credentials or stop running work. Whitelisting suppresses base-tier access-paused billing notifications."}, w.apply)
}
