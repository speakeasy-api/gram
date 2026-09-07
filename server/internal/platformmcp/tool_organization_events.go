//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
)

const (
	defaultOrganizationEventLimit = 20
	maxOrganizationEventLimit     = 50
)

// errOrganizationEventsDisabled is returned when the organization does not
// have Logs enabled. The dashboard Event Feed uses the same gate.
var errOrganizationEventsDisabled = errors.New("platform mcp organization events disabled")

// EventFeedReader is the org-scoped Event Feed page read used by Platform MCP.
// It returns the merged log/span list; this tool never surfaces attributes.
type EventFeedReader interface {
	ListEventLog(ctx context.Context, arg chrepo.ListEventLogParams) ([]chrepo.EventLogRow, error)
}

// EventFeedReadService owns the Event Feed reader, the org Logs gate, and the
// trusted dashboard URL.
type EventFeedReadService struct {
	events       EventFeedReader
	logs         FeatureChecker
	dashboardURL *url.URL
	now          func() time.Time
}

// WithOrganizationEvents enables recent organization-scoped Event Feed summaries.
// A missing Logs checker leaves the live tool unregistered: the dashboard Event
// Feed is gated on that product feature, and a nil checker cannot enforce it.
func (r *PostgresReader) WithOrganizationEvents(events EventFeedReader, logs FeatureChecker, dashboardURL *url.URL) *PostgresReader {
	if r != nil && r.db != nil && events != nil && logs != nil && validDashboardURL(dashboardURL) {
		copyURL := *dashboardURL
		r.eventFeed = &EventFeedReadService{events: events, logs: logs, dashboardURL: &copyURL, now: time.Now}
	}
	return r
}

type ListOrganizationEventsInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum events to return; defaults to 20 and is capped at 50"`
	Window string `json:"window,omitempty" jsonschema:"observation window: 1h, 24h (default), or 7d"`
	Kind   string `json:"kind,omitempty" jsonschema:"optional signal kind filter: log or span"`
}

type OrganizationEvent struct {
	OccurredAt  string `json:"occurred_at"`
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	Name        string `json:"name"`
	BodyPreview string `json:"body_preview,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
}

type ListOrganizationEventsOutput struct {
	Window       ResolvedWindow      `json:"window"`
	Events       []OrganizationEvent `json:"events"`
	More         bool                `json:"more"`
	EventFeedURL string              `json:"event_feed_url"`
}

func (r *PostgresReader) ListOrganizationEvents(ctx context.Context, principal Principal, input ListOrganizationEventsInput) (ListOrganizationEventsOutput, error) {
	if r == nil || r.db == nil || r.eventFeed == nil || r.eventFeed.events == nil || r.eventFeed.logs == nil || r.eventFeed.now == nil {
		return ListOrganizationEventsOutput{}, ErrUnavailable
	}
	enabled, err := r.eventFeed.logs(ctx, principal.OrganizationID)
	if err != nil {
		return ListOrganizationEventsOutput{}, fmt.Errorf("resolve logs feature: %w", err)
	}
	if !enabled {
		return ListOrganizationEventsOutput{}, errOrganizationEventsDisabled
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	if kind != "" && !validOrganizationEventKind(kind) {
		return ListOrganizationEventsOutput{}, fmt.Errorf("kind must be one of log, span")
	}

	now := r.eventFeed.now().UTC()
	window, err := resolveWindow(input.Window, now, eventFeedWindowSpec)
	if err != nil {
		return ListOrganizationEventsOutput{}, err
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultOrganizationEventLimit
	}
	limit = min(limit, maxOrganizationEventLimit)

	kinds := []string(nil)
	if kind != "" {
		kinds = []string{kind}
	}

	rows, err := r.eventFeed.events.ListEventLog(ctx, chrepo.ListEventLogParams{
		EventLogFilters: chrepo.EventLogFilters{
			OrganizationID: principal.OrganizationID,
			TimeStart:      window.start.UnixNano(),
			TimeEnd:        window.end.UnixNano(),
			Kinds:          kinds,
			Sources:        nil,
			Names:          nil,
			Search:         "",
		},
		CursorTimeUnixNano: 0,
		Limit:              limit + 1,
	})
	if err != nil {
		return ListOrganizationEventsOutput{}, fmt.Errorf("list organization events: %w", err)
	}
	rows, more := boundedRows(rows, limit)
	events := make([]OrganizationEvent, 0, len(rows))
	for _, row := range rows {
		events = append(events, OrganizationEvent{
			OccurredAt:  time.Unix(0, row.TimeUnixNano).UTC().Format(time.RFC3339Nano),
			Kind:        row.Kind,
			Source:      row.Source,
			Name:        row.Name,
			BodyPreview: row.BodyPreview,
			ProjectID:   row.ProjectID,
		})
	}
	organization, err := organizationsrepo.New(r.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return ListOrganizationEventsOutput{}, fmt.Errorf("resolve organization events organization: %w", err)
	}
	return ListOrganizationEventsOutput{
		Window:       window,
		Events:       events,
		More:         more,
		EventFeedURL: r.eventFeed.dashboardURL.JoinPath(organization.Slug, "data", "event-feed").String(),
	}, nil
}

func validOrganizationEventKind(kind string) bool {
	switch kind {
	case chrepo.EventKindLog, chrepo.EventKindSpan:
		return true
	default:
		return false
	}
}

func registerOrganizationEventTools(reg *Registrar, reader *PostgresReader) {
	addTool(reg, &mcp.Tool{
		Name:        "list_organization_events",
		Title:       "List Organization Events",
		Description: "List the newest Event Feed entries for the current organization, defaulting to 20 events from the last day. Each event is reduced to when it happened, whether it is a log or a span, its source and name, a short body preview for logs, and the project it belongs to. Constraints: this uses the bounded Event Feed list path and never returns attributes, resource attributes, trace IDs, span IDs, or user identities. The returned dashboard link opens the full Event Feed page.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListOrganizationEventsInput) (*mcp.CallToolResult, ListOrganizationEventsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListOrganizationEventsOutput{}, err
		}
		output, err := reader.ListOrganizationEvents(ctx, principal, input)
		if err != nil {
			if result, ok := organizationEventsToolResult(err); ok {
				return result, ListOrganizationEventsOutput{}, nil
			}
			return nil, ListOrganizationEventsOutput{}, err
		}
		return nil, output, nil
	})
}

func organizationEventsToolResult(err error) (*mcp.CallToolResult, bool) {
	if !errors.Is(err, errOrganizationEventsDisabled) {
		return nil, false
	}
	result := featureUnavailableResult{
		Code:    unavailableCode,
		Feature: "logs",
		Message: "The Event Feed is not enabled for this organization.",
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

func registerUnavailableOrganizationEventTools(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "list_organization_events",
		Title:       "List Organization Events",
		Description: "List recent Event Feed entries for the current organization. This is not switched on for your organization yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, unavailableTool("organization_events"))
}
