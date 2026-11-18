//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// The playbook write assigns only. Clearing an assignment stays a dashboard
// action, so this tool can never leave an organization without a playbook.
const onboardingPlaybookSideEffects = "A shared playbook is copied into a playbook of the organization's own and that copy is assigned, so later edits to the shared playbook never reach the organization; the organization's own playbook is assigned as it is. Records a tenant audit event. The customer's setup wizard walks the assigned steps in their order from its next load, and saved task visibility stops applying while a playbook is assigned. The assignment is refused if the recorded stack no longer supports a step."

// OnboardingPlaybookInput names one playbook to assign to one organization.
type OnboardingPlaybookInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	PlaybookID     string `json:"playbook_id,omitempty" jsonschema:"Exact playbook ID (UUID) to assign. Give this or use_case, not both."`
	UseCase        string `json:"use_case,omitempty" jsonschema:"Use case slug whose default playbook to assign, the one the onboarding survey would pick. Give this or playbook_id, not both."`
}

type onboardingPlaybookStep struct {
	Slug    string
	Applies bool
	Reason  string
}

// onboardingPlaybookState is the organization, what it walks today, and the
// playbook as it reads now against the recorded stack.
type onboardingPlaybookState struct {
	OrganizationID string
	Name           string
	Slug           string
	Assigned       string
	AssignedSteps  []string
	// AlreadyAssigned: the organization walks this playbook, or its own copy
	// of it, so the change would be a no-op.
	AlreadyAssigned bool
	PlaybookID      string
	UseCase         string
	Custom          bool
	Steps           []onboardingPlaybookStep
}

// onboardingPlaybookReceipt names the playbook the organization now walks,
// which is its own copy when a shared playbook (the template) was assigned. It
// uses IDs and slugs only, never staff-authored names or descriptions, matching
// get_organization_onboarding.
type onboardingPlaybookReceipt struct {
	OrganizationID string   `json:"organization_id"`
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	PlaybookID     string   `json:"playbook_id"`
	TemplateID     string   `json:"template_id"`
	UseCase        string   `json:"use_case"`
	Custom         bool     `json:"custom"`
	Steps          []string `json:"steps"`
	Replaced       string   `json:"replaced"`
	ReplacedSteps  []string `json:"replaced_steps"`
	SideEffects    string   `json:"side_effects"`
}

type onboardingPlaybookWriter struct {
	runner *writeRunner
	audit  *audit.Logger
	writes WriteConfig
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
		Assigned: "", AssignedSteps: []string{}, AlreadyAssigned: organizations.OrganizationWalksOnboardingPlaybook(current.Playbook, playbook),
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

func unsupportedSteps(steps []onboardingPlaybookStep) []string {
	problems := make([]string, 0)
	for _, step := range steps {
		if !step.Applies {
			problems = append(problems, step.Slug+" ("+step.Reason+")")
		}
	}
	return problems
}

// apply assigns the playbook through the same path the dashboard uses, which
// re-checks the stack and writes the audit event in this transaction. There is
// no cache to lock or refresh.
func (o *onboardingPlaybookWriter) apply(ctx context.Context, input OnboardingPlaybookInput) (onboardingPlaybookReceipt, error) {
	authority, err := requireWriteAuthority(ctx, o.writes, OperationAssignOrganizationOnboardingPlaybook)
	if err != nil {
		return onboardingPlaybookReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return onboardingPlaybookReceipt{}, err
	}
	rawID, useCase := strings.TrimSpace(input.PlaybookID), strings.TrimSpace(input.UseCase)
	if (rawID == "") == (useCase == "") {
		return onboardingPlaybookReceipt{}, errors.New("provide exactly one of playbook_id and use_case")
	}
	if len(useCase) > 128 {
		return onboardingPlaybookReceipt{}, errors.New("provide an exact use case slug")
	}
	var playbookID uuid.UUID
	if rawID != "" {
		playbookID, err = uuid.Parse(rawID)
		if err != nil {
			return onboardingPlaybookReceipt{}, errors.New("provide an exact playbook ID")
		}
	}
	return runWrite(ctx, o.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (onboardingPlaybookReceipt, error) {
		if useCase != "" {
			playbookID, err = organizations.DefaultOnboardingPlaybookID(ctx, tx, useCase)
			if err != nil {
				return onboardingPlaybookReceipt{}, fmt.Errorf("resolve onboarding use case: %w", err)
			}
		}
		state, err := o.readState(ctx, tx, input.OrganizationID, playbookID)
		if err != nil {
			return onboardingPlaybookReceipt{}, err
		}
		if state.AlreadyAssigned {
			return onboardingPlaybookReceipt{}, errors.New("the organization already walks this playbook; nothing would change")
		}
		if problems := unsupportedSteps(state.Steps); len(problems) > 0 {
			return onboardingPlaybookReceipt{}, fmt.Errorf("the organization's recorded stack does not support: %s", strings.Join(problems, "; "))
		}
		ctx, actor, name := staffMutation(ctx, authority)
		assigned, err := organizations.AssignOrganizationOnboardingPlaybookTx(ctx, tx, o.audit, input.OrganizationID, &playbookID, actor, name)
		if err != nil {
			return onboardingPlaybookReceipt{}, fmt.Errorf("assign onboarding playbook: %w", err)
		}
		if assigned == nil || assigned.Playbook == nil {
			return onboardingPlaybookReceipt{}, errors.New("assign onboarding playbook: nothing was assigned")
		}
		templateID := ""
		if !state.Custom {
			templateID = state.PlaybookID
		}
		return onboardingPlaybookReceipt{
			OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug,
			PlaybookID: assigned.Playbook.ID, TemplateID: templateID, UseCase: state.UseCase, Custom: state.Custom, Steps: playbookStepSlugs(state.Steps),
			Replaced: state.Assigned, ReplacedSteps: state.AssignedSteps, SideEffects: onboardingPlaybookSideEffects,
		}, nil
	})
}

func (o *onboardingPlaybookWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationAssignOrganizationOnboardingPlaybook), Title: "Assign Organization Onboarding Playbook", Description: "Assign an onboarding playbook to one exact organization, named by its ID or by the use case whose default playbook the onboarding survey would assign. A shared playbook is a template: the organization gets and walks a copy of its own. The assignment is checked against the organization's recorded stack and refused when a step is unsupported."}, o.apply)
}
