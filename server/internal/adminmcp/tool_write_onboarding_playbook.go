//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// The playbook write assigns only. Clearing an assignment stays a dashboard
// action, so an approved proposal can never leave an organization without a
// playbook.
const onboardingPlaybookSideEffects = "Assigns the playbook to the organization and records a tenant audit event. The customer's setup wizard walks the playbook's steps in its order from its next load, and saved task visibility stops applying while a playbook is assigned. The assignment is refused if the recorded stack no longer supports a step."

type PrepareOnboardingPlaybookInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	PlaybookID     string `json:"playbook_id,omitempty" jsonschema:"Exact playbook ID (UUID) to assign. Give this or use_case, not both."`
	UseCase        string `json:"use_case,omitempty" jsonschema:"Use case slug whose default playbook to assign, the one the onboarding survey would pick. Give this or playbook_id, not both."`
	RetryKey       string `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
}

type onboardingPlaybookChange struct {
	PlaybookID string `json:"playbook_id"`
}

type onboardingPlaybookStep struct {
	Slug    string `json:"slug"`
	Applies bool   `json:"applies"`
	Reason  string `json:"reason,omitempty"`
}

// onboardingPlaybookState is digested as the proposal's expected state: the
// organization, what it walks today, and the playbook as it reads now against
// the recorded stack. An edit to the playbook's steps, a stack change or a
// competing assignment makes the proposal stale.
type onboardingPlaybookState struct {
	OrganizationID string                   `json:"organization_id"`
	Name           string                   `json:"name"`
	Slug           string                   `json:"slug"`
	Assigned       string                   `json:"assigned"`
	AssignedSteps  []string                 `json:"assigned_steps"`
	PlaybookID     string                   `json:"playbook_id"`
	UseCase        string                   `json:"use_case"`
	Custom         bool                     `json:"custom"`
	Steps          []onboardingPlaybookStep `json:"steps"`
}

// onboardingPlaybookPreview uses IDs and slugs only, never staff-authored
// names or descriptions, matching get_organization_onboarding.
type onboardingPlaybookPreview struct {
	OrganizationID string   `json:"organization_id"`
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	PlaybookID     string   `json:"playbook_id"`
	UseCase        string   `json:"use_case"`
	Custom         bool     `json:"custom"`
	Steps          []string `json:"steps"`
	Replaces       string   `json:"replaces"`
	ReplacesSteps  []string `json:"replaces_steps"`
	SideEffects    string   `json:"side_effects"`
}

type onboardingPlaybookReceipt struct {
	PlaybookID string   `json:"playbook_id"`
	Replaced   string   `json:"replaced"`
	Steps      []string `json:"steps"`
}

type onboardingPlaybookWriter struct {
	store   *proposalStore
	audit   *audit.Logger
	writes  WriteConfig
	baseURL string
}

func playbookStepSlugs(steps []onboardingPlaybookStep) []string {
	slugs := make([]string, 0, len(steps))
	for _, step := range steps {
		slugs = append(slugs, step.Slug)
	}
	return slugs
}

// readState locks the organization row that every assignment locks, then reads
// the current assignment and the candidate playbook under it. A playbook that
// cannot be assigned comes back as the bad request assigning it would raise.
func (o *onboardingPlaybookWriter) readState(ctx context.Context, tx pgx.Tx, organizationID string, playbookID uuid.UUID) (onboardingPlaybookState, error) {
	org, err := orgrepo.New(tx).LockOrganizationForSetupTaskUpdate(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && org.ID != organizationID) {
		return onboardingPlaybookState{}, ErrStaleState
	}
	if err != nil {
		return onboardingPlaybookState{}, fmt.Errorf("lock onboarding playbook target: %w", err)
	}
	current, err := organizations.LoadOrganizationOnboardingPlaybook(ctx, tx, organizationID)
	if err != nil {
		return onboardingPlaybookState{}, fmt.Errorf("read onboarding playbook assignment: %w", err)
	}
	playbook, applicability, err := organizations.CheckOrganizationOnboardingPlaybook(ctx, tx, organizationID, playbookID)
	if err != nil {
		return onboardingPlaybookState{}, fmt.Errorf("read onboarding playbook: %w", err)
	}
	state := onboardingPlaybookState{
		OrganizationID: org.ID, Name: org.Name, Slug: org.Slug,
		Assigned: "", AssignedSteps: []string{},
		PlaybookID: playbook.ID, UseCase: "", Custom: playbook.OrganizationID != nil,
		Steps: make([]onboardingPlaybookStep, 0, len(applicability)),
	}
	if playbook.UseCaseSlug != nil {
		state.UseCase = *playbook.UseCaseSlug
	}
	if current.Playbook != nil {
		state.Assigned = current.Playbook.ID
		for _, step := range current.Playbook.Steps {
			state.AssignedSteps = append(state.AssignedSteps, step.Slug)
		}
	}
	for _, step := range applicability {
		state.Steps = append(state.Steps, onboardingPlaybookStep{Slug: step.Slug, Applies: step.Applies, Reason: step.Reason})
	}
	return state, nil
}

func (o *onboardingPlaybookWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) (onboardingPlaybookState, error) {
	if proposal.Operation != OperationAssignOrganizationOnboardingPlaybook || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return onboardingPlaybookState{}, ErrProposalInvalidated
	}
	var change onboardingPlaybookChange
	if err := json.Unmarshal(proposal.Arguments, &change); err != nil {
		return onboardingPlaybookState{}, ErrProposalInvalidated
	}
	playbookID, err := uuid.Parse(change.PlaybookID)
	if err != nil {
		return onboardingPlaybookState{}, ErrProposalInvalidated
	}
	state, err := o.readState(ctx, tx, proposal.Target.OrganizationID, playbookID)
	if err != nil {
		// A playbook retired, or moved to another organization, since prepare.
		if _, ok := errors.AsType[*oops.ShareableError](err); ok {
			return onboardingPlaybookState{}, ErrStaleState
		}
		return onboardingPlaybookState{}, err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return onboardingPlaybookState{}, fmt.Errorf("encode expected onboarding playbook state: %w", err)
	}
	digest, err := stateDigest(encoded)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return onboardingPlaybookState{}, ErrStaleState
	}
	if state.Assigned == state.PlaybookID || len(unsupportedSteps(state.Steps)) > 0 {
		return onboardingPlaybookState{}, ErrProposalInvalidated
	}
	return state, nil
}

func unsupportedSteps(steps []onboardingPlaybookStep) []string {
	problems := make([]string, 0)
	for _, step := range steps {
		if !step.Applies {
			problems = append(problems, step.Slug+" ("+step.Reason+")")
		}
	}
	return problems
}

func (o *onboardingPlaybookWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	_, err := o.expectedState(ctx, tx, proposal)
	return err
}

func (o *onboardingPlaybookWriter) prepare(ctx context.Context, input PrepareOnboardingPlaybookInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, o.writes, OperationAssignOrganizationOnboardingPlaybook)
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
	rawID, useCase := strings.TrimSpace(input.PlaybookID), strings.TrimSpace(input.UseCase)
	if (rawID == "") == (useCase == "") {
		return ProposalOutput{}, errors.New("provide exactly one of playbook_id and use_case")
	}
	if len(useCase) > 128 {
		return ProposalOutput{}, errors.New("provide an exact use case slug")
	}
	var playbookID uuid.UUID
	if rawID != "" {
		playbookID, err = uuid.Parse(rawID)
		if err != nil {
			return ProposalOutput{}, errors.New("provide an exact playbook ID")
		}
	}
	tx, err := o.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin onboarding playbook proposal preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if useCase != "" {
		playbookID, err = organizations.DefaultOnboardingPlaybookID(ctx, tx, useCase)
		if err != nil {
			return ProposalOutput{}, fmt.Errorf("resolve onboarding use case: %w", err)
		}
	}
	state, err := o.readState(ctx, tx, input.OrganizationID, playbookID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish onboarding playbook proposal preparation: %w", err)
	}
	if state.Assigned == state.PlaybookID {
		return ProposalOutput{}, errors.New("this playbook is already assigned to the organization; nothing would change")
	}
	if problems := unsupportedSteps(state.Steps); len(problems) > 0 {
		return ProposalOutput{}, fmt.Errorf("the organization's recorded stack does not support: %s", strings.Join(problems, "; "))
	}
	args, err := json.Marshal(onboardingPlaybookChange{PlaybookID: state.PlaybookID})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode onboarding playbook change: %w", err)
	}
	expected, err := json.Marshal(state)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode expected onboarding playbook state: %w", err)
	}
	preview, err := json.Marshal(onboardingPlaybookPreview{
		OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug,
		PlaybookID: state.PlaybookID, UseCase: state.UseCase, Custom: state.Custom, Steps: playbookStepSlugs(state.Steps),
		Replaces: state.Assigned, ReplacesSteps: state.AssignedSteps, SideEffects: onboardingPlaybookSideEffects,
	})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode onboarding playbook preview: %w", err)
	}
	proposal, replay, err := o.store.Create(ctx, owner, NewProposal{Operation: OperationAssignOrganizationOnboardingPlaybook, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: state.OrganizationID}, IdempotencyKey: input.RetryKey, Arguments: args, ExpectedState: expected, Preview: preview}, time.Now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, o.baseURL), nil
}

// view renders the stored preview for the approval page. A preview that does
// not name the proposal's own organization is never shown.
func (o *onboardingPlaybookWriter) view(proposal Proposal) (proposalView, error) {
	var preview onboardingPlaybookPreview
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.OrganizationID == "" || preview.OrganizationID != proposal.Target.OrganizationID || preview.PlaybookID == "" || len(preview.Steps) == 0 {
		return proposalView{}, ErrProposalInvalidated
	}
	kind := "the custom playbook"
	if !preview.Custom {
		kind = "the shared playbook"
		if preview.UseCase != "" {
			kind += " of use case " + preview.UseCase
		}
	}
	before, beforeSteps := "None", "None"
	if preview.Replaces != "" {
		before = preview.Replaces
		beforeSteps = strings.Join(preview.ReplacesSteps, ", ")
	}
	return proposalView{
		Summary:          fmt.Sprintf("Assign %s %s with %d steps", kind, preview.PlaybookID, len(preview.Steps)),
		OrganizationID:   preview.OrganizationID,
		OrganizationName: preview.Name,
		OrganizationSlug: preview.Slug,
		Changes: []proposalViewChange{
			{Setting: "Playbook", Before: before, After: preview.PlaybookID},
			{Setting: "Steps", Before: beforeSteps, After: strings.Join(preview.Steps, ", ")},
		},
		SideEffects: preview.SideEffects,
	}, nil
}

// execution assigns the stored playbook through the same path the dashboard
// uses, which re-checks the stack and writes the audit event in the proposal's
// transaction. There is no cache to lock or refresh.
func (o *onboardingPlaybookWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{
		Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
			state, err := o.expectedState(ctx, tx, proposal)
			if err != nil {
				return "", nil, err
			}
			playbookID, err := uuid.Parse(state.PlaybookID)
			if err != nil {
				return "", nil, ErrProposalInvalidated
			}
			ctx, actor, name := staffMutation(ctx, authority)
			if _, err := organizations.AssignOrganizationOnboardingPlaybookTx(ctx, tx, o.audit, proposal.Target.OrganizationID, &playbookID, actor, name); err != nil {
				return "", nil, fmt.Errorf("assign onboarding playbook: %w", err)
			}
			result, err := json.Marshal(onboardingPlaybookReceipt{PlaybookID: state.PlaybookID, Replaced: state.Assigned, Steps: playbookStepSlugs(state.Steps)})
			if err != nil {
				return "", nil, fmt.Errorf("encode onboarding playbook receipt: %w", err)
			}
			return "succeeded", result, nil
		},
	}
}

func (o *onboardingPlaybookWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_assign_organization_onboarding_playbook", Title: "Prepare Organization Onboarding Playbook Assignment", Description: "Prepare an exact, single-organization change that assigns an onboarding playbook, named by its ID or by the use case whose default playbook the onboarding survey would assign. The assignment is checked against the organization's recorded stack and refused when a step is unsupported. Returns a server-stored preview and a private staff approval URL. Does not make the change. Requires admin:write."}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareOnboardingPlaybookInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := o.prepare(ctx, input)
		return nil, out, err
	})
}
