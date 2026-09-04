package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginassignments "github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const operationSetPluginAssignments = "set_plugin_assignments"

var (
	ErrPluginAssignmentMutationUnavailable = errors.New("platform mcp plugin assignment mutations unavailable")
	ErrPluginAssignmentMutationInvalid     = errors.New("invalid platform mcp plugin assignment mutation")
	ErrPluginAssignmentMutationNotFound    = errors.New("platform mcp plugin assignment not found")
	ErrPluginAssignmentMutationConflict    = errors.New("platform mcp plugin assignment mutation conflict")
)

type PluginAssignmentMutationError struct {
	Code    string
	Message string
	Cause   error
}

func (e *PluginAssignmentMutationError) Error() string { return e.Message }
func (e *PluginAssignmentMutationError) Unwrap() error { return e.Cause }

type SetPluginAssignmentsInput struct {
	ProjectID                 string   `json:"project_id" jsonschema:"explicit project ID that owns the plugin"`
	Plugin                    string   `json:"plugin" jsonschema:"exact plugin ID, slug, or name returned by list_plugins"`
	AssignmentReferences      []string `json:"assignment_references" jsonschema:"complete desired set of opaque references returned by list_plugin_assignments or get_plugin; an empty set removes every assignment"`
	ExpectedAssignmentVersion string   `json:"expected_assignment_version" jsonschema:"assignment version returned by get_plugin immediately before this write"`
	IdempotencyKey            string   `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact write"`
	Confirmed                 bool     `json:"confirmed" jsonschema:"set true only after the user explicitly confirms the complete assignment replacement for this exact plugin"`
}

type PluginAssignmentSummaryResult struct {
	Kind        string        `json:"kind"`
	DisplayName string        `json:"display_name"`
	MemberCount *SubjectCount `json:"member_count,omitempty"`
}

type PluginAssignmentMutationPlugin struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Slug        string                  `json:"slug"`
	IsDefault   bool                    `json:"is_default"`
	Assignments PluginAssignmentSummary `json:"assignments"`
	Publication string                  `json:"publication"`
}

type SetPluginAssignmentsReceiptResult struct {
	ProjectID         string                          `json:"project_id"`
	Plugin            PluginAssignmentMutationPlugin  `json:"plugin"`
	AssignmentVersion string                          `json:"assignment_version"`
	Assignments       []PluginAssignmentSummaryResult `json:"assignments"`
	ResultCategory    string                          `json:"result_category"`
}

type SetPluginAssignmentsOutput struct {
	SetPluginAssignmentsReceiptResult
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

type normalizedSetPluginAssignments struct {
	ProjectID                 string   `json:"project_id"`
	Plugin                    string   `json:"plugin"`
	AssignmentReferences      []string `json:"assignment_references"`
	ExpectedAssignmentVersion string   `json:"expected_assignment_version"`
}

// WithAssignmentMutations enables the separately gated write half of the plugin
// service. Inventory reads remain available when any write dependency is absent.
func (s *PluginsService) WithAssignmentMutations(flags feature.Provider, organizations OrganizationSlugResolver, logger *audit.Logger, budget OperationBudget) *PluginsService {
	if s == nil {
		return nil
	}
	s.mutationFlags = flags
	s.organizations = organizations
	s.audit = logger
	s.mutationBudget = budget
	s.mutationReceipts = NewPluginAssignmentMutationReceiptStore(s.db)
	return s
}

func (s *PluginsService) mutationValid() bool {
	return s.valid() && s.mutationFlags != nil && s.organizations != nil && s.audit != nil && s.mutationBudget.valid() && s.mutationReceipts != nil
}

func (s *PluginsService) SetPluginAssignments(ctx context.Context, principal Principal, input SetPluginAssignmentsInput) (SetPluginAssignmentsOutput, error) {
	if !s.mutationValid() {
		return SetPluginAssignmentsOutput{}, pluginAssignmentMutationUnavailable(nil)
	}
	if err := requirePluginAssignmentConfirmation(input.Confirmed); err != nil {
		return SetPluginAssignmentsOutput{}, err
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Plugin = strings.TrimSpace(input.Plugin)
	input.ExpectedAssignmentVersion = strings.TrimSpace(input.ExpectedAssignmentVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if principal.UserID == "" || input.ProjectID == "" || input.Plugin == "" || input.ExpectedAssignmentVersion == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || len(input.AssignmentReferences) > maxPluginMembers {
		return SetPluginAssignmentsOutput{}, pluginAssignmentMutationInvalid("The plugin assignment request is invalid.")
	}
	if pluginID, err := uuid.Parse(input.Plugin); err == nil && pluginID == uuid.Nil {
		return SetPluginAssignmentsOutput{}, pluginAssignmentMutationInvalid("The plugin ID must not be all zeroes.")
	}

	project, err := s.resolveProject(ctx, platformrepo.New(s.db), principal, input.ProjectID)
	if err != nil {
		return SetPluginAssignmentsOutput{}, err
	}
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || organizationSlug == "" {
		return SetPluginAssignmentsOutput{}, pluginAssignmentMutationUnavailable(err)
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.mutationFlags, feature.FlagPlatformMCPPluginAssignmentMutations, principal.OrganizationID, feature.OrgProjectGroups(organizationSlug, project.Slug))
	if err != nil {
		return SetPluginAssignmentsOutput{}, pluginAssignmentMutationUnavailable(err)
	}
	if evaluation != feature.EvaluationEnabled {
		return SetPluginAssignmentsOutput{}, pluginAssignmentMutationUnavailable(nil)
	}
	if err := s.mutationBudget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return SetPluginAssignmentsOutput{}, &PluginAssignmentMutationError{Code: "rate_limited", Message: "The plugin assignment mutation rate limit was reached.", Cause: err}
		}
		return SetPluginAssignmentsOutput{}, pluginAssignmentMutationUnavailable(err)
	}
	references, err := normalizePluginAssignmentReferences(input.AssignmentReferences)
	if err != nil {
		return SetPluginAssignmentsOutput{}, err
	}
	normalized := normalizedPluginAssignmentMutationInput(project.ID, input.Plugin, references, input.ExpectedAssignmentVersion)
	receipt, err := s.mutationReceipts.Execute(ctx, principal, project, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (SetPluginAssignmentsReceiptResult, error) {
		target, err := s.resolve(ctx, platformrepo.New(tx), principal, project.ID, input.Plugin)
		if err != nil {
			return SetPluginAssignmentsReceiptResult{}, err
		}
		locked, err := pluginassignments.Lock(ctx, tx, principal.OrganizationID, project.ID, target.ID)
		if errors.Is(err, pluginassignments.ErrNotFound) {
			return SetPluginAssignmentsReceiptResult{}, pluginAssignmentMutationNotFound()
		}
		if err != nil {
			return SetPluginAssignmentsReceiptResult{}, pluginAssignmentMutationUnavailable(err)
		}
		principalURNs, summaries, err := s.resolveMutationAssignments(ctx, tx, principal, project, references)
		if err != nil {
			return SetPluginAssignmentsReceiptResult{}, err
		}
		result, err := pluginassignments.Replace(ctx, tx, s.audit, locked, pluginassignments.Input{
			OrganizationID:   principal.OrganizationID,
			ProjectID:        project.ID,
			PluginID:         target.ID,
			PrincipalURNs:    principalURNs,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
			ActorDisplayName: nil,
			ActorSlug:        nil,
		}, func(ctx context.Context, _ pluginsrepo.Plugin, current, _ []string) error {
			if pluginAssignmentVersion(s.assignmentVersionKey, project.ID, target.ID, current) != input.ExpectedAssignmentVersion {
				return pluginAssignmentMutationConflict("The plugin assignments changed after they were read. Read the plugin again and retry with the new assignment version.")
			}
			if len(current) > maxPluginMembers {
				return pluginAssignmentMutationInvalid("This plugin has too many current assignments to replace safely here. Use the dashboard.")
			}
			visible, err := visiblePluginAssignments(ctx, tx, principal.OrganizationID, current)
			if err != nil {
				return err
			}
			for _, currentURN := range current {
				if _, ok := visible[canonicalPluginAssignmentURN(currentURN)]; !ok {
					return pluginAssignmentMutationInvalid("This plugin has a current assignment that cannot be shown safely here. Use the dashboard.")
				}
			}
			return nil
		})
		if err != nil {
			switch {
			case errors.Is(err, pluginassignments.ErrNotFound):
				return SetPluginAssignmentsReceiptResult{}, pluginAssignmentMutationNotFound()
			case errors.Is(err, pluginassignments.ErrInvalid):
				return SetPluginAssignmentsReceiptResult{}, pluginAssignmentMutationInvalid("The selected plugin assignments are no longer valid.")
			default:
				return SetPluginAssignmentsReceiptResult{}, fmt.Errorf("replace plugin assignments: %w", err)
			}
		}
		row, err := platformrepo.New(tx).GetPlatformMCPPluginInventoryItem(ctx, platformrepo.GetPlatformMCPPluginInventoryItemParams{
			PluginID: target.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID,
		})
		if err != nil {
			return SetPluginAssignmentsReceiptResult{}, fmt.Errorf("read committed plugin assignment state: %w", err)
		}
		inventory := pluginFromInventoryRow(platformrepo.ListPlatformMCPPluginInventoryRow(row))
		return SetPluginAssignmentsReceiptResult{
			ProjectID: project.ID.String(),
			Plugin: PluginAssignmentMutationPlugin{
				ID: inventory.ID, Name: inventory.Name, Slug: inventory.Slug, IsDefault: inventory.IsDefault,
				Assignments: inventory.Assignments, Publication: inventory.Publication,
			},
			AssignmentVersion: pluginAssignmentVersion(s.assignmentVersionKey, project.ID, target.ID, result.PrincipalURNs),
			Assignments:       summaries,
			ResultCategory:    "updated",
		}, nil
	})
	if err != nil {
		return SetPluginAssignmentsOutput{}, err
	}
	var result SetPluginAssignmentsReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return SetPluginAssignmentsOutput{}, fmt.Errorf("decode plugin assignment mutation receipt: %w", err)
	}
	return SetPluginAssignmentsOutput{SetPluginAssignmentsReceiptResult: result, Receipt: riskMutationToolReceipt(receipt)}, nil
}

func (s *PluginsService) resolveMutationAssignments(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, references []string) ([]string, []PluginAssignmentSummaryResult, error) {
	if len(references) == 0 {
		return []string{}, []PluginAssignmentSummaryResult{}, nil
	}
	principalURNs := make([]string, 0, len(references))
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		value, err := s.assignmentReferences.DecodeScoped(reference, principal, subjectKindPluginAssignment, project.ID.String(), s.now().UTC())
		if err != nil {
			return nil, nil, pluginAssignmentMutationNotFound()
		}
		canonical := canonicalPluginAssignmentURN(value)
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		principalURNs = append(principalURNs, canonical)
	}

	rows, err := platformrepo.New(tx).ListPlatformMCPPluginAssignmentOptions(ctx, platformrepo.ListPlatformMCPPluginAssignmentOptionsParams{
		SelectedPrincipalUrns: principalURNs,
		ResultLimit:           maxPluginMembers + 1,
		OrganizationID:        principal.OrganizationID,
	})
	if err != nil {
		return nil, nil, pluginAssignmentMutationUnavailable(err)
	}
	byURN := make(map[string]platformrepo.ListPlatformMCPPluginAssignmentOptionsRow, len(rows))
	for _, row := range rows {
		byURN[canonicalPluginAssignmentURN(row.PrincipalUrn)] = row
	}
	summaries := make([]PluginAssignmentSummaryResult, 0, len(principalURNs))
	for _, principalURN := range principalURNs {
		row, ok := byURN[principalURN]
		if !ok {
			return nil, nil, pluginAssignmentMutationNotFound()
		}
		var count *SubjectCount
		if row.MemberCount.Valid {
			value := NewSubjectCount(row.MemberCount.Int64)
			count = &value
		}
		summaries = append(summaries, PluginAssignmentSummaryResult{Kind: row.Kind, DisplayName: row.DisplayName, MemberCount: count})
	}
	slices.SortFunc(summaries, func(a, b PluginAssignmentSummaryResult) int {
		if compared := strings.Compare(a.Kind, b.Kind); compared != 0 {
			return compared
		}
		return strings.Compare(a.DisplayName, b.DisplayName)
	})
	return principalURNs, summaries, nil
}

func visiblePluginAssignments(ctx context.Context, tx pgx.Tx, organizationID string, principalURNs []string) (map[string]struct{}, error) {
	if len(principalURNs) == 0 {
		return map[string]struct{}{}, nil
	}
	canonical := make([]string, len(principalURNs))
	for index, principalURN := range principalURNs {
		canonical[index] = canonicalPluginAssignmentURN(principalURN)
	}
	rows, err := platformrepo.New(tx).ListPlatformMCPPluginAssignmentOptions(ctx, platformrepo.ListPlatformMCPPluginAssignmentOptionsParams{
		SelectedPrincipalUrns: canonical,
		ResultLimit:           maxPluginMembers + 1,
		OrganizationID:        organizationID,
	})
	if err != nil {
		return nil, pluginAssignmentMutationUnavailable(err)
	}
	visible := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		visible[canonicalPluginAssignmentURN(row.PrincipalUrn)] = struct{}{}
	}
	return visible, nil
}

func requirePluginAssignmentConfirmation(confirmed bool) error {
	if confirmed {
		return nil
	}
	return &PluginAssignmentMutationError{Code: "confirmation_required", Message: "Ask the user to confirm the complete assignment replacement for this exact plugin, then retry with confirmed: true.", Cause: ErrPluginAssignmentMutationInvalid}
}

func normalizePluginAssignmentReferences(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	references := slices.Clone(raw)
	for i := range references {
		references[i] = strings.TrimSpace(references[i])
		if references[i] == "" {
			return nil, pluginAssignmentMutationInvalid("Every assignment reference must be non-empty.")
		}
	}
	slices.Sort(references)
	return slices.Compact(references), nil
}

func normalizedPluginAssignmentMutationInput(projectID uuid.UUID, plugin string, references []string, expectedVersion string) normalizedSetPluginAssignments {
	return normalizedSetPluginAssignments{
		ProjectID: projectID.String(), Plugin: plugin, AssignmentReferences: slices.Clone(references), ExpectedAssignmentVersion: expectedVersion,
	}
}

func pluginAssignmentMutationInvalid(message string) error {
	return &PluginAssignmentMutationError{Code: "invalid_request", Message: message, Cause: ErrPluginAssignmentMutationInvalid}
}

func pluginAssignmentMutationNotFound() error {
	return &PluginAssignmentMutationError{Code: "not_found", Message: "One or more selected plugin assignments are unavailable. List the assignments again and choose from the current result.", Cause: ErrPluginAssignmentMutationNotFound}
}

func pluginAssignmentMutationConflict(message string) error {
	return &PluginAssignmentMutationError{Code: "conflict", Message: message, Cause: ErrPluginAssignmentMutationConflict}
}

func pluginAssignmentMutationUnavailable(cause error) error {
	if cause == nil {
		return &PluginAssignmentMutationError{Code: unavailableCode, Message: "Plugin assignment changes are not enabled for this project.", Cause: ErrPluginAssignmentMutationUnavailable}
	}
	return &PluginAssignmentMutationError{Code: unavailableCode, Message: "Plugin assignment changes are temporarily unavailable.", Cause: fmt.Errorf("%w: %w", ErrPluginAssignmentMutationUnavailable, cause)}
}
