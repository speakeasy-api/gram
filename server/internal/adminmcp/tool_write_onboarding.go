//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// The onboarding write changes task visibility only. The preset is never
// written because it cannot be reset to unset, so a preset change would not
// be reversible through this tool.
const onboardingSideEffects = "Changes which setup tasks appear on the organisation's onboarding board and records tenant audit events. The onboarding preset is not changed. Saving pins every task's visibility, so later changes to task defaults won't apply to this organisation."

type PrepareOnboardingInput struct {
	OrganizationID  string   `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	VisibleTaskKeys []string `json:"visible_task_keys" jsonschema:"Complete list of setup task keys to show, from get_organization_onboarding. Every other task is hidden; an empty list hides all tasks."`
	RetryKey        string   `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
}

type onboardingChange struct {
	VisibleTaskKeys []string `json:"visible_task_keys"`
}

type onboardingTaskState struct {
	Key    string `json:"key"`
	Hidden bool   `json:"hidden"`
}

// onboardingState is digested as the proposal's expected state. Tasks are in
// catalogue order, so a catalogue change between prepare and execute also
// makes the proposal stale.
type onboardingState struct {
	OrganizationID string                `json:"organization_id"`
	Name           string                `json:"name"`
	Slug           string                `json:"slug"`
	Preset         *string               `json:"preset"`
	Tasks          []onboardingTaskState `json:"tasks"`
}

// onboardingPreview uses task keys only, not customer-facing titles, matching
// get_organization_onboarding.
type onboardingPreview struct {
	OrganizationID string   `json:"organization_id"`
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	Show           []string `json:"show"`
	Hide           []string `json:"hide"`
	Unchanged      int      `json:"unchanged"`
	SideEffects    string   `json:"side_effects"`
}

type onboardingReceipt struct {
	Shown  []string `json:"shown"`
	Hidden []string `json:"hidden"`
}

// onboardingDiff is a validated selection against one onboarding state.
type onboardingDiff struct {
	// Visible is the selection, sorted so equal selections digest equally.
	Visible []string
	// Show and Hide list the tasks that change, in catalogue order.
	Show      []string
	Hide      []string
	Unchanged int
}

func diffOnboarding(state onboardingState, keys []string) (onboardingDiff, error) {
	if keys == nil {
		return onboardingDiff{}, errors.New("provide visible_task_keys as an explicit list; an empty list hides every task")
	}
	if len(keys) > len(state.Tasks) {
		return onboardingDiff{}, errors.New("visible_task_keys lists more keys than there are setup tasks")
	}
	known := make(map[string]bool, len(state.Tasks))
	for _, task := range state.Tasks {
		known[task.Key] = true
	}
	selected := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !known[key] {
			return onboardingDiff{}, errors.New("visible_task_keys contains an unknown setup task key; use keys from get_organization_onboarding")
		}
		if selected[key] {
			return onboardingDiff{}, errors.New("visible_task_keys contains a duplicate key")
		}
		selected[key] = true
	}
	visible := slices.Clone(keys)
	slices.Sort(visible)
	diff := onboardingDiff{Visible: visible, Show: []string{}, Hide: []string{}, Unchanged: 0}
	for _, task := range state.Tasks {
		switch {
		case task.Hidden && selected[task.Key]:
			diff.Show = append(diff.Show, task.Key)
		case !task.Hidden && !selected[task.Key]:
			diff.Hide = append(diff.Hide, task.Key)
		default:
			diff.Unchanged++
		}
	}
	return diff, nil
}

type onboardingWriter struct {
	store   *proposalStore
	audit   *audit.Logger
	writes  WriteConfig
	baseURL string
}

// readState locks the organization row that every onboarding save locks, then
// reads the effective task visibility under it.
func (o *onboardingWriter) readState(ctx context.Context, tx pgx.Tx, organizationID string) (onboardingState, error) {
	org, err := orgrepo.New(tx).LockOrganizationForSetupTaskUpdate(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && org.ID != organizationID) {
		return onboardingState{}, ErrStaleState
	}
	if err != nil {
		return onboardingState{}, fmt.Errorf("lock onboarding target: %w", err)
	}
	config, err := organizations.LoadOnboardingConfiguration(ctx, tx, organizationID)
	if err != nil {
		return onboardingState{}, fmt.Errorf("read onboarding state: %w", err)
	}
	tasks := make([]onboardingTaskState, 0, len(config.Tasks))
	for _, task := range config.Tasks {
		// A group's visibility follows its cards and cannot be selected.
		if task.Group {
			continue
		}
		tasks = append(tasks, onboardingTaskState{Key: task.Key, Hidden: task.Hidden})
	}
	return onboardingState{OrganizationID: org.ID, Name: org.Name, Slug: org.Slug, Preset: config.Preset, Tasks: tasks}, nil
}

func (o *onboardingWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) (onboardingDiff, error) {
	if proposal.Operation != OperationSetOrganizationOnboarding || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return onboardingDiff{}, ErrProposalInvalidated
	}
	var change onboardingChange
	if err := json.Unmarshal(proposal.Arguments, &change); err != nil || change.VisibleTaskKeys == nil {
		return onboardingDiff{}, ErrProposalInvalidated
	}
	state, err := o.readState(ctx, tx, proposal.Target.OrganizationID)
	if err != nil {
		return onboardingDiff{}, err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return onboardingDiff{}, fmt.Errorf("encode expected onboarding state: %w", err)
	}
	digest, err := stateDigest(encoded)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return onboardingDiff{}, ErrStaleState
	}
	diff, err := diffOnboarding(state, change.VisibleTaskKeys)
	if err != nil || len(diff.Show)+len(diff.Hide) == 0 {
		return onboardingDiff{}, ErrProposalInvalidated
	}
	return diff, nil
}

func (o *onboardingWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	_, err := o.expectedState(ctx, tx, proposal)
	return err
}

func (o *onboardingWriter) prepare(ctx context.Context, input PrepareOnboardingInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, o.writes, OperationSetOrganizationOnboarding)
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
	if input.VisibleTaskKeys == nil {
		return ProposalOutput{}, errors.New("provide visible_task_keys as an explicit list; an empty list hides every task")
	}
	tx, err := o.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin onboarding proposal preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	state, err := o.readState(ctx, tx, input.OrganizationID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish onboarding proposal preparation: %w", err)
	}
	diff, err := diffOnboarding(state, input.VisibleTaskKeys)
	if err != nil {
		return ProposalOutput{}, err
	}
	if len(diff.Show)+len(diff.Hide) == 0 {
		return ProposalOutput{}, errors.New("this selection matches the organization's current onboarding tasks; nothing would change")
	}
	args, err := json.Marshal(onboardingChange{VisibleTaskKeys: diff.Visible})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode onboarding change: %w", err)
	}
	expected, err := json.Marshal(state)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode expected onboarding state: %w", err)
	}
	preview, err := json.Marshal(onboardingPreview{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, Show: diff.Show, Hide: diff.Hide, Unchanged: diff.Unchanged, SideEffects: onboardingSideEffects})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode onboarding preview: %w", err)
	}
	proposal, replay, err := o.store.Create(ctx, owner, NewProposal{Operation: OperationSetOrganizationOnboarding, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: state.OrganizationID}, IdempotencyKey: input.RetryKey, Arguments: args, ExpectedState: expected, Preview: preview}, time.Now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, o.baseURL), nil
}

// view renders the stored preview for the approval page. A preview that does
// not name the proposal's own organization is never shown.
func (o *onboardingWriter) view(proposal Proposal) (proposalView, error) {
	var preview onboardingPreview
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.OrganizationID == "" || preview.OrganizationID != proposal.Target.OrganizationID || len(preview.Show)+len(preview.Hide) == 0 {
		return proposalView{}, ErrProposalInvalidated
	}
	changes := make([]proposalViewChange, 0, len(preview.Show)+len(preview.Hide))
	for _, key := range preview.Show {
		changes = append(changes, proposalViewChange{Setting: key + " task", Before: "Hidden", After: "Shown"})
	}
	for _, key := range preview.Hide {
		changes = append(changes, proposalViewChange{Setting: key + " task", Before: "Shown", After: "Hidden"})
	}
	return proposalView{
		Summary:          fmt.Sprintf("Show %d and hide %d onboarding tasks; %d unchanged", len(preview.Show), len(preview.Hide), preview.Unchanged),
		OrganizationID:   preview.OrganizationID,
		OrganizationName: preview.Name,
		OrganizationSlug: preview.Slug,
		Changes:          changes,
		SideEffects:      preview.SideEffects,
	}, nil
}

// execution saves the stored selection with the preset left unset, so the
// domain save never writes it. There is no cache to lock or refresh.
func (o *onboardingWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{
		Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
			diff, err := o.expectedState(ctx, tx, proposal)
			if err != nil {
				return "", nil, err
			}
			ctx, actor, name := staffMutation(ctx, authority)
			if _, err := organizations.SaveOnboardingConfigurationTx(ctx, tx, o.audit, proposal.Target.OrganizationID, diff.Visible, nil, actor, name); err != nil {
				return "", nil, fmt.Errorf("save onboarding task visibility: %w", err)
			}
			result, err := json.Marshal(onboardingReceipt{Shown: diff.Show, Hidden: diff.Hide})
			if err != nil {
				return "", nil, fmt.Errorf("encode onboarding receipt: %w", err)
			}
			return "succeeded", result, nil
		},
	}
}

func (o *onboardingWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_set_organization_onboarding", Title: "Prepare Organization Onboarding Task Change", Description: "Prepare an exact, single-organization change to which onboarding setup tasks are visible. visible_task_keys is the complete list to show, using keys from get_organization_onboarding; every other task is hidden. Never changes the onboarding preset. Returns a server-stored preview and a private staff approval URL. Does not make the change. Requires admin:write."}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareOnboardingInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := o.prepare(ctx, input)
		return nil, out, err
	})
}
