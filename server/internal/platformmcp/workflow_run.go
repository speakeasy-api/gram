package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	// workflowRunItemEvent carries one item per event so a run can be
	// broken down by outcome without unpacking an array.
	workflowRunItemEvent = "platform_mcp_workflow_run_item"

	// workflowRunEvent carries the totals for one run, so a run that
	// handled nothing still leaves a trace.
	workflowRunEvent = "platform_mcp_workflow_run"

	workflowRunMaxItems    = 200
	workflowRunMaxEndpoint = 2048
	workflowRunMaxText     = 1000
)

// ErrWorkflowRunInvalid reports a report the tool contract rejects
// before anything is emitted.
var ErrWorkflowRunInvalid = errors.New("invalid platform mcp workflow run report")

// workflowRunOutcomes is what a workflow reports doing with one item,
// matching the words the shipped workflows already report to the user.
//
// added_unverified exists because a workflow can complete an add it cannot
// prove: a catalogue registration whose effective configuration the server does
// not read back is neither a proven add nor a failure, and reporting it as
// either makes the analytics lie in one direction or the other.
var workflowRunOutcomes = []string{"added", "added_unverified", "already_present", "blocked", "failed"}

// WorkflowRunEmitter delivers one diagnostics event to Speakeasy's own
// analytics pipeline. It is deliberately narrow: this package never sees the
// vendor client's types.
type WorkflowRunEmitter interface {
	CaptureEvent(ctx context.Context, eventName string, distinctID string, eventProperties map[string]any) error
}

// WorkflowRunItem is one thing a workflow run handled.
type WorkflowRunItem struct {
	// Name is the non-secret name the workflow knows this item by.
	Name string `json:"name" jsonschema:"non-secret name this workflow knows the item by"`

	// Kind is the declared transport or type, usually the reason an excluded
	// item was excluded.
	Kind string `json:"kind,omitempty" jsonschema:"optional item kind, for example streamable_http, sse, or stdio"`

	// Endpoint is the safe remote endpoint when the item has one. It is a
	// separate validated field rather than free text so a credential cannot
	// reach analytics inside a URL.
	Endpoint string `json:"endpoint,omitempty" jsonschema:"safe https endpoint when the item has one; never include credentials, headers, or tokens"`

	// Outcome is what the run did with this item.
	Outcome string `json:"outcome" jsonschema:"one of added, added_unverified, already_present, blocked, failed"`

	// Reason explains a blocked or failed outcome in the workflow's own words.
	Reason string `json:"reason,omitempty" jsonschema:"short explanation for a blocked or failed outcome, at most 1000 characters"`
}

// WorkflowRunInput is one run of one shipped Platform MCP workflow.
type WorkflowRunInput struct {
	// Skill names the workflow that ran.
	Skill string `json:"skill" jsonschema:"name of the workflow that ran, for example add-existing-mcp-servers"`

	// RunID groups every event from one run. The caller generates it.
	RunID string `json:"run_id" jsonschema:"caller-generated identifier grouping this run's events, at most 128 characters"`

	// ProjectSlug is the project the run targeted.
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"project slug the run targeted, when the workflow targets one"`

	// Client names the local client the run was driven from.
	Client string `json:"client,omitempty" jsonschema:"local client the run was driven from, for example claude_code"`

	// Items is everything the run handled, including what it excluded. A run
	// that handled nothing sends none, which is itself a reportable result.
	Items []WorkflowRunItem `json:"items" jsonschema:"everything the run handled, including the items it excluded; send an empty list when the run handled nothing"`
}

// WorkflowRunService records one run of a shipped Platform MCP workflow
// for Speakeasy's own debugging. It is internal telemetry about how a workflow
// behaves in the field: nothing it emits is readable back through the product.
type WorkflowRunService struct {
	logger  *slog.Logger
	emitter WorkflowRunEmitter
}

func NewWorkflowRunService(logger *slog.Logger, emitter WorkflowRunEmitter) *WorkflowRunService {
	return &WorkflowRunService{logger: logger, emitter: emitter}
}

func (s *WorkflowRunService) valid() bool {
	return s != nil && s.emitter != nil
}

// Record validates one run report and emits it. An endpoint that fails the
// credential contract is dropped from its item rather than rejecting the whole
// report: the items most worth seeing are the ones a workflow excluded.
func (s *WorkflowRunService) Record(ctx context.Context, principal Principal, input WorkflowRunInput) error {
	if principal.OrganizationID == "" || !validWorkflowRun(input) {
		return ErrWorkflowRunInvalid
	}

	identity := map[string]any{
		"skill":           input.Skill,
		"run_id":          input.RunID,
		"project_slug":    input.ProjectSlug,
		"client":          input.Client,
		"organization_id": principal.OrganizationID,
	}

	outcomes := map[string]int{}
	for _, item := range input.Items {
		outcomes[item.Outcome]++

		endpoint := sanitizeWorkflowRunEndpoint(item.Endpoint)

		properties := map[string]any{
			"name":     item.Name,
			"kind":     item.Kind,
			"endpoint": endpoint,
			"outcome":  item.Outcome,
			"reason":   item.Reason,
		}
		maps.Copy(properties, identity)

		if err := s.emitter.CaptureEvent(ctx, workflowRunItemEvent, principal.OrganizationID, properties); err != nil {
			return fmt.Errorf("emit workflow run item event: %w", err)
		}
	}

	runProperties := map[string]any{"items_total": len(input.Items)}
	maps.Copy(runProperties, identity)
	for _, outcome := range workflowRunOutcomes {
		runProperties["items_"+outcome] = outcomes[outcome]
	}
	if err := s.emitter.CaptureEvent(ctx, workflowRunEvent, principal.OrganizationID, runProperties); err != nil {
		return fmt.Errorf("emit workflow run summary event: %w", err)
	}

	return nil
}

func validWorkflowRun(input WorkflowRunInput) bool {
	if strings.TrimSpace(input.Skill) == "" || !safeWorkflowRunText(input.Skill, 128) {
		return false
	}
	if strings.TrimSpace(input.RunID) == "" || !safeWorkflowRunText(input.RunID, 128) {
		return false
	}
	if !safeWorkflowRunText(input.ProjectSlug, 128) || !safeWorkflowRunText(input.Client, 64) {
		return false
	}
	if len(input.Items) > workflowRunMaxItems {
		return false
	}
	for _, item := range input.Items {
		if strings.TrimSpace(item.Name) == "" || !safeWorkflowRunText(item.Name, 200) {
			return false
		}
		if !safeWorkflowRunText(item.Kind, 64) || !safeWorkflowRunText(item.Reason, workflowRunMaxText) {
			return false
		}
		if !slices.Contains(workflowRunOutcomes, item.Outcome) {
			return false
		}
	}
	return true
}

// sanitizeWorkflowRunEndpoint reduces an endpoint to scheme, host and path,
// and returns empty for anything that is not a plain https URL.
//
// Everything else is discarded rather than inspected. A query string or
// fragment can carry a credential in any number of shapes — an unrecognised
// parameter name, a fragment the URL parser ignores, a pair its splitter drops
// — and diagnostics only need to identify which server was seen. Dropping the
// whole tail is exact; a denylist of parameter names is a guess.
func sanitizeWorkflowRunEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}
	if len([]rune(endpoint)) > workflowRunMaxEndpoint || strings.ContainsFunc(endpoint, unicode.IsSpace) {
		return ""
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}).String()
}

func safeWorkflowRunText(value string, maxRunes int) bool {
	if value == "" {
		return true
	}
	if len([]rune(value)) > maxRunes {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool {
		return r != '\n' && r != '\t' && unicode.IsControl(r)
	}) < 0
}

// logEmitFailure records a diagnostics emit failure, which is never allowed to
// fail the workflow that reported it.
func (s *WorkflowRunService) logEmitFailure(ctx context.Context, organizationID string, err error) {
	s.logger.ErrorContext(ctx, "emit platform mcp workflow run report",
		attr.SlogEvent("platform_mcp_workflow_run_emit_failed"),
		attr.SlogOrganizationID(organizationID),
		attr.SlogError(err),
	)
}
