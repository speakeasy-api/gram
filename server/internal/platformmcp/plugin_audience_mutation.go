package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	pluginassignments "github.com/speakeasy-api/gram/server/internal/plugins/assignments"
	"github.com/speakeasy-api/gram/server/internal/plugins/installmode"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
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
	ProjectID                 string            `json:"project_id" jsonschema:"explicit project ID that owns the plugin"`
	Plugin                    string            `json:"plugin" jsonschema:"exact plugin ID, slug, or name returned by list_plugins"`
	AssignmentReferences      []string          `json:"assignment_references" jsonschema:"complete desired set of opaque references returned by list_plugin_assignments or get_plugin; an empty set removes every assignment"`
	ExpectedAssignmentVersion string            `json:"expected_assignment_version" jsonschema:"assignment version returned by get_plugin immediately before this write"`
	InstallModes              map[string]string `json:"install_modes,omitempty" jsonschema:"optional install mode per reference in assignment_references: required (installed, users can't turn it off), default (installed, users can turn it off) or available (not installed until a user turns it on); a reference left out keeps its current mode, or default when newly assigned"`
	IdempotencyKey            string            `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact write"`
	Confirmed                 bool              `json:"confirmed" jsonschema:"set true only after the user explicitly confirms the complete assignment replacement for this exact plugin"`
}

type PluginAssignmentSummaryResult struct {
	Kind        string        `json:"kind"`
	DisplayName string        `json:"display_name"`
	MemberCount *SubjectCount `json:"member_count,omitempty"`

	// InstallMode is the audience's stored install mode after the write.
	InstallMode string `json:"install_mode,omitempty"`
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
	ProjectID          string                          `json:"project_id"`
	Plugin             PluginAssignmentMutationPlugin  `json:"plugin"`
	AssignmentVersion  string                          `json:"assignment_version"`
	Assignments        []PluginAssignmentSummaryResult `json:"assignments"`
	ResultCategory     string                          `json:"result_category"`
	PublicationRequest string                          `json:"publication_request,omitempty"`
}

type SetPluginAssignmentsOutput struct {
	SetPluginAssignmentsReceiptResult
	Receipt RiskMutationToolReceipt `json:"receipt"`
}

type normalizedSetPluginAssignments struct {
	ProjectID                 string            `json:"project_id"`
	Plugin                    string            `json:"plugin"`
	AssignmentReferences      []string          `json:"assignment_references"`
	ExpectedAssignmentVersion string            `json:"expected_assignment_version"`
	InstallModes              map[string]string `json:"install_modes,omitempty"`
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

func (s *PluginsService) WithDistributionAdmission(guard *admission.Guard) *PluginsService {
	if s != nil {
		s.distributionAdmission = guard
	}
	return s
}

func (s *PluginsService) mutationValid() bool {
	return s.valid() && s.mutationFlags != nil && s.organizations != nil && s.audit != nil && s.mutationBudget.valid() && s.mutationReceipts != nil && s.distributionAdmission != nil
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
	if principal.UserID == "" || input.ProjectID == "" || input.Plugin == "" || input.ExpectedAssignmentVersion == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || len(input.AssignmentReferences) > maxPluginMembers || len(input.InstallModes) > maxPluginMembers {
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
	var rollout admission.RolloutConfig
	var rolloutErr error
	rollout, rolloutErr = s.distributionAdmission.Resolve(ctx, principal.OrganizationID, organizationSlug, project.Slug)
	ctx = roledelivery.WithProjectAdmission(ctx, principal.OrganizationID, project.ID, rollout, rolloutErr)
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
	requestedModes, err := normalizePluginAssignmentInstallModes(input.InstallModes, references)
	if err != nil {
		return SetPluginAssignmentsOutput{}, err
	}
	normalized := normalizedPluginAssignmentMutationInput(project.ID, input.Plugin, references, input.ExpectedAssignmentVersion, requestedModes)
	receipt, err := s.mutationReceipts.Execute(ctx, principal, project, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (SetPluginAssignmentsReceiptResult, error) {
		if err := admission.LockProject(ctx, tx, project.ID); err != nil {
			return SetPluginAssignmentsReceiptResult{}, pluginAssignmentMutationUnavailable(err)
		}
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
		principalURNs, summaries, summaryURNs, err := s.resolveMutationAssignments(ctx, tx, principal, project, references)
		if err != nil {
			return SetPluginAssignmentsReceiptResult{}, err
		}
		installModes, err := s.resolveMutationInstallModes(principal, project, requestedModes)
		if err != nil {
			return SetPluginAssignmentsReceiptResult{}, err
		}
		result, err := pluginassignments.Replace(ctx, tx, s.audit, locked, pluginassignments.Input{
			OrganizationID:   principal.OrganizationID,
			ProjectID:        project.ID,
			PluginID:         target.ID,
			PrincipalURNs:    principalURNs,
			InstallModes:     installModes,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
			ActorDisplayName: nil,
			ActorSlug:        nil,
		}, pluginassignments.Dependencies{
			DeliveryGuard: s.distributionAdmission,
			Guard: func(ctx context.Context, tx pgx.Tx, plugin pluginsrepo.Plugin, current, desired []string) error {
				if pluginassignments.IsSubset(desired, current) {
					return nil
				}
				return pluginAssignmentAdmissionError(s.distributionAdmission.CheckPluginAudience(ctx, tx, rollout, rolloutErr, principal.OrganizationID, project.ID, plugin.ID, desired))
			},
			BeforeReplace: func(ctx context.Context, _ pluginsrepo.Plugin, current, _ []string, currentModes map[string]installmode.Mode) error {
				if pluginAssignmentVersion(s.assignmentVersionKey, project.ID, target.ID, current, currentModes) != input.ExpectedAssignmentVersion {
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
			},
		})
		if err != nil {
			switch {
			case errors.Is(err, pluginassignments.ErrNotFound):
				return SetPluginAssignmentsReceiptResult{}, pluginAssignmentMutationNotFound()
			case errors.Is(err, admission.ErrApprovalRequired), errors.Is(err, admission.ErrPrivateGatewayAudience), errors.Is(err, admission.ErrDistributionDisabled), errors.Is(err, admission.ErrUnavailable):
				return SetPluginAssignmentsReceiptResult{}, pluginAssignmentAdmissionError(err)
			case errors.Is(err, pluginassignments.ErrInvalid):
				return SetPluginAssignmentsReceiptResult{}, pluginAssignmentMutationInvalid("The selected plugin assignments are no longer valid.")
			default:
				return SetPluginAssignmentsReceiptResult{}, fmt.Errorf("replace plugin assignments: %w", err)
			}
		}
		for index := range summaries {
			summaries[index].InstallMode = string(result.InstallModes[summaryURNs[index]])
		}
		var publicationRequest string
		if result.ContentChanged {
			outcome, err := s.publicationRequests.ProjectWithOutcome(ctx, tx, principal.OrganizationID, project.ID, principal.UserID)
			if err != nil {
				return SetPluginAssignmentsReceiptResult{}, fmt.Errorf("request role audience publication: %w", err)
			}
			publicationRequest = string(outcome)
		}
		row, err := platformrepo.New(tx).GetPlatformMCPPluginInventoryItem(ctx, platformrepo.GetPlatformMCPPluginInventoryItemParams{
			PluginID: target.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID,
		})
		if err != nil {
			return SetPluginAssignmentsReceiptResult{}, fmt.Errorf("read committed plugin assignment state: %w", err)
		}
		inventory := pluginFromInventoryRow(platformrepo.ListPlatformMCPPluginInventoryRow(row))
		if inventory.Assignments == nil {
			return SetPluginAssignmentsReceiptResult{}, pluginAssignmentMutationUnavailable(errors.New("plugin assignment summary unavailable"))
		}
		return SetPluginAssignmentsReceiptResult{
			ProjectID: project.ID.String(),
			Plugin: PluginAssignmentMutationPlugin{
				ID: inventory.ID, Name: inventory.Name, Slug: inventory.Slug, IsDefault: inventory.IsDefault,
				Assignments: *inventory.Assignments, Publication: inventory.Publication,
			},
			AssignmentVersion:  pluginAssignmentVersion(s.assignmentVersionKey, project.ID, target.ID, result.PrincipalURNs, result.InstallModes),
			Assignments:        summaries,
			ResultCategory:     "updated",
			PublicationRequest: publicationRequest,
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

func pluginAssignmentAdmissionError(err error) error {
	switch {
	case errors.Is(err, admission.ErrApprovalRequired):
		return &PluginAssignmentMutationError{Code: "approval_required", Message: "This MCP server does not have approval for the plugin's complete audience. Review the current audience and approval, then try again.", Cause: err}
	case errors.Is(err, admission.ErrPrivateGatewayAudience):
		return &PluginAssignmentMutationError{Code: "conflict", Message: "A private-only gateway cannot be distributed to Everyone. Choose a scoped plugin audience or change the gateway's network access.", Cause: err}
	case errors.Is(err, admission.ErrDistributionDisabled):
		return &PluginAssignmentMutationError{Code: "distribution_disabled", Message: "Direct-remote distribution is temporarily disabled. Existing audiences can still be narrowed.", Cause: err}
	case errors.Is(err, admission.ErrUnavailable):
		return pluginAssignmentMutationUnavailable(err)
	default:
		return err
	}
}

// resolveMutationAssignments returns the canonical principals, their sorted
// summaries, and the principal behind each summary at the same index.
func (s *PluginsService) resolveMutationAssignments(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, references []string) ([]string, []PluginAssignmentSummaryResult, []string, error) {
	if len(references) == 0 {
		return []string{}, []PluginAssignmentSummaryResult{}, []string{}, nil
	}
	principalURNs := make([]string, 0, len(references))
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		value, err := s.assignmentReferences.DecodeScoped(reference, principal, subjectKindPluginAssignment, project.ID.String(), s.now().UTC())
		if err != nil {
			return nil, nil, nil, pluginAssignmentMutationNotFound()
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
		return nil, nil, nil, pluginAssignmentMutationUnavailable(err)
	}
	byURN := make(map[string]platformrepo.ListPlatformMCPPluginAssignmentOptionsRow, len(rows))
	for _, row := range rows {
		byURN[canonicalPluginAssignmentURN(row.PrincipalUrn)] = row
	}
	type summaryForPrincipal struct {
		summary      PluginAssignmentSummaryResult
		principalURN string
	}
	entries := make([]summaryForPrincipal, 0, len(principalURNs))
	for _, principalURN := range principalURNs {
		row, ok := byURN[principalURN]
		if !ok {
			return nil, nil, nil, pluginAssignmentMutationNotFound()
		}
		var count *SubjectCount
		if row.MemberCount.Valid {
			value := NewSubjectCount(row.MemberCount.Int64)
			count = &value
		}
		entries = append(entries, summaryForPrincipal{
			summary:      PluginAssignmentSummaryResult{Kind: row.Kind, DisplayName: row.DisplayName, MemberCount: count, InstallMode: ""},
			principalURN: principalURN,
		})
	}
	slices.SortFunc(entries, func(a, b summaryForPrincipal) int {
		if compared := strings.Compare(a.summary.Kind, b.summary.Kind); compared != 0 {
			return compared
		}
		return strings.Compare(a.summary.DisplayName, b.summary.DisplayName)
	})
	summaries := make([]PluginAssignmentSummaryResult, 0, len(entries))
	summaryURNs := make([]string, 0, len(entries))
	for _, entry := range entries {
		summaries = append(summaries, entry.summary)
		summaryURNs = append(summaryURNs, entry.principalURN)
	}
	return principalURNs, summaries, summaryURNs, nil
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

func normalizedPluginAssignmentMutationInput(projectID uuid.UUID, plugin string, references []string, expectedVersion string, installModes map[string]string) normalizedSetPluginAssignments {
	return normalizedSetPluginAssignments{
		ProjectID: projectID.String(), Plugin: plugin, AssignmentReferences: slices.Clone(references), ExpectedAssignmentVersion: expectedVersion,
		InstallModes: maps.Clone(installModes),
	}
}

// normalizePluginAssignmentInstallModes trims each reference key and validates
// its mode. Every key must name one of the normalized assignment references.
func normalizePluginAssignmentInstallModes(raw map[string]string, references []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	modes := make(map[string]string, len(raw))
	for rawReference, rawMode := range raw {
		reference := strings.TrimSpace(rawReference)
		if !slices.Contains(references, reference) {
			return nil, pluginAssignmentMutationInvalid("Every install mode must name a reference in assignment_references.")
		}
		mode, err := installmode.Parse(strings.TrimSpace(rawMode))
		if err != nil {
			return nil, pluginAssignmentMutationInvalid("Install modes must be required, default, or available.")
		}
		if previous, ok := modes[reference]; ok && previous != string(mode) {
			return nil, pluginAssignmentMutationInvalid("A reference was given two different install modes.")
		}
		modes[reference] = string(mode)
	}
	return modes, nil
}

// resolveMutationInstallModes decodes reference-keyed install modes into the
// principal-keyed form the shared assignment write expects. Distinct references
// to one principal must agree, or the stored mode would depend on map order.
func (s *PluginsService) resolveMutationInstallModes(principal Principal, project ResolvedProject, modes map[string]string) (map[string]string, error) {
	if len(modes) == 0 {
		return nil, nil
	}
	resolved := make(map[string]string, len(modes))
	for reference, mode := range modes {
		value, err := s.assignmentReferences.DecodeScoped(reference, principal, subjectKindPluginAssignment, project.ID.String(), s.now().UTC())
		if err != nil {
			return nil, pluginAssignmentMutationNotFound()
		}
		principalURN := canonicalPluginAssignmentURN(value)
		if previous, ok := resolved[principalURN]; ok && previous != mode {
			return nil, pluginAssignmentMutationInvalid("Two references to the same assignment were given different install modes.")
		}
		resolved[principalURN] = mode
	}
	return resolved, nil
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
