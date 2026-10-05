//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

const organizationWhitelistSideEffects = "Changes the dashboard's demo-access gate when session information is refreshed. Removing whitelisting may show the demo gate or existing trial-ended screen; it is not credential revocation and does not stop in-flight work. Whitelisting suppresses base-tier access-paused billing notifications. Does not change account type, subscriptions, trial records, provider keys or disabled state. Organisations with any trial record, regardless of tier or lifecycle state, are unavailable for this MCP operation. Existing signup, billing and trial lifecycle flows may change whitelisting independently. Records a tenant audit event."

var errWhitelistTrial = errors.New("organisations with a trial record are unavailable for this whitelist operation; use the staff dashboard")

// PrepareOrganizationWhitelistInput describes one exact demo-gate change.
type PrepareOrganizationWhitelistInput struct {
	// OrganizationID is a canonical ID, never a name or slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`

	// Whitelisted is the desired demo-gate setting.
	Whitelisted bool `json:"whitelisted" jsonschema:"Whether to bypass the dashboard demo-access gate; does not enable a disabled organization or revoke credentials"`

	// RetryKey identifies this exact change.
	RetryKey string `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
}

type organizationWhitelistChange struct {
	Whitelisted bool `json:"whitelisted"`
}

type organizationWhitelistPreview struct {
	Target      admin.OrganizationWhitelistState `json:"target"`
	After       bool                             `json:"whitelisted_after"`
	SideEffects string                           `json:"side_effects"`
}

type organizationWhitelistReceipt struct {
	OrganizationID string `json:"organization_id"`
	Whitelisted    bool   `json:"whitelisted"`
}

type organizationWhitelistWriter struct {
	store   *proposalStore
	audit   *audit.Logger
	writes  WriteConfig
	baseURL string
	now     func() time.Time
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

func (w *organizationWhitelistWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) (organizationWhitelistChange, error) {
	if proposal.Operation != OperationSetOrganizationWhitelist || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceKind != "" || proposal.Target.ResourceID != "" {
		return organizationWhitelistChange{}, ErrProposalInvalidated
	}
	var change organizationWhitelistChange
	if err := json.Unmarshal(proposal.Arguments, &change); err != nil {
		return organizationWhitelistChange{}, ErrProposalInvalidated
	}
	state, err := w.readState(ctx, tx, proposal.Target.OrganizationID)
	if errors.Is(err, errWhitelistTrial) {
		return organizationWhitelistChange{}, ErrStaleState
	}
	if err != nil {
		return organizationWhitelistChange{}, err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return organizationWhitelistChange{}, fmt.Errorf("encode expected whitelist state: %w", err)
	}
	digest, err := stateDigest(encoded)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return organizationWhitelistChange{}, ErrStaleState
	}
	if state.Whitelisted == change.Whitelisted {
		return organizationWhitelistChange{}, ErrProposalInvalidated
	}
	return change, nil
}

func (w *organizationWhitelistWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	_, err := w.expectedState(ctx, tx, proposal)
	return err
}

func (w *organizationWhitelistWriter) prepare(ctx context.Context, input PrepareOrganizationWhitelistInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationSetOrganizationWhitelist)
	if err != nil {
		return ProposalOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := checkPrepareTarget(input.OrganizationID, input.RetryKey); err != nil {
		return ProposalOutput{}, err
	}
	tx, err := w.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin whitelist proposal preparation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	state, err := w.readState(ctx, tx, input.OrganizationID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish whitelist proposal preparation: %w", err)
	}
	if state.Whitelisted == input.Whitelisted {
		return ProposalOutput{}, errors.New("organization already has the requested whitelist setting; nothing would change")
	}
	expected, err := json.Marshal(state)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode expected whitelist state: %w", err)
	}
	arguments, err := json.Marshal(organizationWhitelistChange{Whitelisted: input.Whitelisted})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode whitelist change: %w", err)
	}
	preview, err := json.Marshal(organizationWhitelistPreview{Target: state, After: input.Whitelisted, SideEffects: organizationWhitelistSideEffects})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode whitelist preview: %w", err)
	}
	proposal, replay, err := w.store.Create(ctx, owner, NewProposal{Operation: OperationSetOrganizationWhitelist, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: state.OrganizationID}, IdempotencyKey: input.RetryKey, Arguments: arguments, ExpectedState: expected, Preview: preview}, w.now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, w.baseURL), nil
}

func (w *organizationWhitelistWriter) view(proposal Proposal) (proposalView, error) {
	if proposal.Operation != OperationSetOrganizationWhitelist || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceKind != "" || proposal.Target.ResourceID != "" {
		return proposalView{}, ErrProposalInvalidated
	}
	var preview organizationWhitelistPreview
	var change organizationWhitelistChange
	if err := json.Unmarshal(proposal.Arguments, &change); err != nil {
		return proposalView{}, ErrProposalInvalidated
	}
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.Target.OrganizationID != proposal.Target.OrganizationID || preview.Target.Whitelisted == preview.After || preview.After != change.Whitelisted {
		return proposalView{}, ErrProposalInvalidated
	}
	return proposalView{
		Summary: "Change organization demo-access whitelisting", OrganizationID: preview.Target.OrganizationID,
		OrganizationName: preview.Target.Name, OrganizationSlug: preview.Target.Slug,
		Changes: []proposalViewChange{
			{Setting: "Whitelisted", Before: strconv.FormatBool(preview.Target.Whitelisted), After: strconv.FormatBool(preview.After)},
			{Setting: "Account type (unchanged)", Before: preview.Target.AccountType, After: preview.Target.AccountType},
			{Setting: "Organization disabled (unchanged)", Before: strconv.FormatBool(preview.Target.DisabledAt != nil), After: strconv.FormatBool(preview.Target.DisabledAt != nil)},
		}, SideEffects: organizationWhitelistSideEffects,
	}, nil
}

func (w *organizationWhitelistWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
		change, err := w.expectedState(ctx, tx, proposal)
		if err != nil {
			return "", nil, err
		}
		ctx, actor, displayName := staffMutation(ctx, authority)
		after, err := admin.SetOrganizationWhitelistTx(ctx, tx, w.audit, proposal.Target.OrganizationID, change.Whitelisted, actor, displayName)
		if err != nil {
			return "", nil, fmt.Errorf("set organization whitelist: %w", err)
		}
		result, err := json.Marshal(organizationWhitelistReceipt{OrganizationID: proposal.Target.OrganizationID, Whitelisted: after})
		if err != nil {
			return "", nil, fmt.Errorf("encode whitelist receipt: %w", err)
		}
		return "succeeded", result, nil
	}}
}

func (w *organizationWhitelistWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_set_organization_whitelist", Title: "Prepare Organization Whitelist Change", Description: "Prepare changing the dashboard demo-access whitelist for one exact canonical organization ID without any trial record, regardless of tier or lifecycle state. Does not change account type, subscriptions, trial records or disabled state, revoke credentials or stop running work. Whitelisting suppresses base-tier access-paused billing notifications. Returns a stored before/after preview and private same-staff browser approval URL; does not make the change. Requires admin:write."}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareOrganizationWhitelistInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := w.prepare(ctx, input)
		return nil, out, err
	})
}
