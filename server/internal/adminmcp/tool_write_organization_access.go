//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
)

const organizationAccessSideEffects = "Disabling blocks organisation access according to the dashboard's existing behaviour. It does not cancel subscriptions, change provider keys or immediately terminate in-flight work."

// PrepareOrganizationAccessInput identifies one exact organisation and retry.
type PrepareOrganizationAccessInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	RetryKey       string `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
}

type organizationAccessExpectedState struct {
	OrganizationID string     `json:"organization_id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	DisabledAt     *time.Time `json:"disabled_at"`
}

type organizationAccessPreview struct {
	OrganizationID string     `json:"organization_id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	EnabledBefore  bool       `json:"enabled_before"`
	EnabledAfter   bool       `json:"enabled_after"`
	DisabledAt     *time.Time `json:"disabled_at"`
	SideEffects    string     `json:"side_effects"`
}

type organizationAccessReceipt struct {
	OrganizationID string `json:"organization_id"`
	Enabled        bool   `json:"enabled"`
}

// organizationAccessWriter handles exactly one of the existing dashboard
// operations through a guarded proposal and durable receipt.
type organizationAccessWriter struct {
	store     *proposalStore
	audit     *audit.Logger
	writes    WriteConfig
	baseURL   string
	operation WriteOperation
}

func (w *organizationAccessWriter) readState(ctx context.Context, tx pgx.Tx, organizationID string) (organizationAccessExpectedState, error) {
	state, err := admin.LockOrganizationAccessTx(ctx, tx, organizationID)
	if errors.Is(err, admin.ErrOrganizationAccessNotFound) {
		return organizationAccessExpectedState{}, ErrStaleState
	}
	if err != nil {
		return organizationAccessExpectedState{}, fmt.Errorf("read organization access state: %w", err)
	}
	return organizationAccessExpectedState{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, DisabledAt: state.DisabledAt}, nil
}

func (w *organizationAccessWriter) desiredEnabled() bool {
	return w.operation == OperationEnableOrganization
}

func (w *organizationAccessWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	if proposal.Operation != w.operation || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return ErrProposalInvalidated
	}
	state, err := w.readState(ctx, tx, proposal.Target.OrganizationID)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode expected organization access state: %w", err)
	}
	digest, err := stateDigest(encoded)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return ErrStaleState
	}
	if (state.DisabledAt == nil) == w.desiredEnabled() {
		return ErrProposalInvalidated
	}
	return nil
}

func (w *organizationAccessWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	return w.expectedState(ctx, tx, proposal)
}

func (w *organizationAccessWriter) prepare(ctx context.Context, input PrepareOrganizationAccessInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, w.operation)
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
		return ProposalOutput{}, fmt.Errorf("begin organization access proposal preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	state, err := w.readState(ctx, tx, input.OrganizationID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish organization access proposal preparation: %w", err)
	}
	enabledBefore := state.DisabledAt == nil
	enabledAfter := w.desiredEnabled()
	if enabledBefore == enabledAfter {
		return ProposalOutput{}, errors.New("organization already has the requested access state; nothing would change")
	}
	expected, err := json.Marshal(state)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode expected organization access state: %w", err)
	}
	preview, err := json.Marshal(organizationAccessPreview{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, EnabledBefore: enabledBefore, EnabledAfter: enabledAfter, DisabledAt: state.DisabledAt, SideEffects: organizationAccessSideEffects})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode organization access preview: %w", err)
	}
	proposal, replay, err := w.store.Create(ctx, owner, NewProposal{Operation: w.operation, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: state.OrganizationID}, IdempotencyKey: input.RetryKey, Arguments: json.RawMessage(`{}`), ExpectedState: expected, Preview: preview}, time.Now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, w.baseURL), nil
}

func (w *organizationAccessWriter) view(proposal Proposal) (proposalView, error) {
	if proposal.Operation != w.operation || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.ProjectID.Valid || proposal.Target.ResourceKind != "" || proposal.Target.ResourceID != "" {
		return proposalView{}, ErrProposalInvalidated
	}
	var preview organizationAccessPreview
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.OrganizationID == "" || preview.OrganizationID != proposal.Target.OrganizationID || preview.EnabledBefore == preview.EnabledAfter || preview.EnabledAfter != w.desiredEnabled() {
		return proposalView{}, ErrProposalInvalidated
	}
	accessBefore, accessAfter := "Disabled", "Enabled"
	if preview.EnabledBefore {
		accessBefore, accessAfter = "Enabled", "Disabled"
	}
	verb := "enable"
	if !preview.EnabledAfter {
		verb = "disable"
	}
	return proposalView{
		Summary: fmt.Sprintf("%s organization access", verb), OrganizationID: preview.OrganizationID,
		OrganizationName: preview.Name, OrganizationSlug: preview.Slug,
		Changes:     []proposalViewChange{{Setting: "Organization access", Before: accessBefore, After: accessAfter}},
		SideEffects: preview.SideEffects,
	}, nil
}

func (w *organizationAccessWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
		if err := w.expectedState(ctx, tx, proposal); err != nil {
			return "", nil, err
		}
		ctx, actor, displayName := staffMutation(ctx, authority)
		state, err := admin.SetOrganizationAccessTx(ctx, tx, w.audit, proposal.Target.OrganizationID, w.desiredEnabled(), actor, displayName)
		if errors.Is(err, admin.ErrOrganizationAccessNotFound) {
			return "", nil, ErrStaleState
		}
		if err != nil {
			return "", nil, fmt.Errorf("set organization access: %w", err)
		}
		result, err := json.Marshal(organizationAccessReceipt{OrganizationID: state.OrganizationID, Enabled: w.desiredEnabled()})
		if err != nil {
			return "", nil, fmt.Errorf("encode organization access receipt: %w", err)
		}
		return "succeeded", result, nil
	}}
}

func (w *organizationAccessWriter) registerPrepare(server *mcp.Server) {
	name := "prepare_enable_organization"
	title := "Prepare Organization Enable"
	description := "Prepare enabling access for one exact canonical organization ID. Returns a server-stored before/after preview and a private staff approval URL. Does not make the change. Requires admin:write."
	if !w.desiredEnabled() {
		name = "prepare_disable_organization"
		title = "Prepare Organization Disable"
		description = "Prepare disabling access for one exact canonical organization ID. Disabling blocks access according to existing dashboard behaviour, but does not cancel subscriptions, change provider keys or immediately terminate in-flight work. Returns a server-stored before/after preview and a private staff approval URL. Does not make the change. Requires admin:write."
	}
	mcp.AddTool(server, &mcp.Tool{Name: name, Title: title, Description: description}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareOrganizationAccessInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := w.prepare(ctx, input)
		return nil, out, err
	})
}
