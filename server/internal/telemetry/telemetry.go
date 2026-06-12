package telemetry

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// EventSource identifies the type of event that generated a telemetry log.
type EventSource string

const (
	EventSourceToolCall       EventSource = "tool_call"
	EventSourceChatCompletion EventSource = "chat_completion"
	EventSourceEvaluation     EventSource = "evaluation"
	EventSourceResourceRead   EventSource = "resource_read"
	EventSourceHook           EventSource = "hook"
	EventSourceAPI            EventSource = "api"
	EventSourceTrigger        EventSource = "trigger"
	EventSourceAssistant      EventSource = "assistant"
)

// PosthogClient defines the interface for capturing events in PostHog.
type PosthogClient interface {
	CaptureEvent(ctx context.Context, eventName string, distinctID string, eventProperties map[string]any) error
}

// FeatureChecker is a function to determine whether a feature is enabled for an organization.
type FeatureChecker func(ctx context.Context, organisationID string) (bool, error)

// ToolInfo represents the minimal tool information needed for logging
type ToolInfo struct {
	ID             string
	URN            string
	Name           string
	ProjectID      string
	DeploymentID   string
	FunctionID     *string
	OrganizationID string
}

// UserAttributes is the v0 allowlist of directory attributes stamped onto
// telemetry rows: the struct fields are the allowlist. They are WorkOS
// predefined attributes (https://workos.com/docs/directory-sync/attributes):
// named and schematized by WorkOS, auto-mapped across directory providers, so
// they mean the same thing for every organization. Customer-defined custom
// attributes are deliberately excluded for now; Postgres keeps the full
// payload, so expanding this later only requires hydrating new rows.
type UserAttributes struct {
	DepartmentName string `json:"department_name,omitempty"`
	JobTitle       string `json:"job_title,omitempty"`
	EmployeeType   string `json:"employee_type,omitempty"`
	DivisionName   string `json:"division_name,omitempty"`
	CostCenterName string `json:"cost_center_name,omitempty"`
}

func (a UserAttributes) IsZero() bool {
	return a == UserAttributes{}
}

// UserInfo identifies the user a telemetry log row is attributed to, plus the
// point-in-time directory context stamped alongside. Callers provide identity
// (UserID, Email) on LogParams instead of stamping user identity keys into
// the attributes map directly; the logger fills the directory-derived parts
// (Attributes, Groups, Roles) during hydration and merges everything into the
// row's attributes.
type UserInfo struct {
	UserID string
	Email  string

	// Attributes is the allowlisted subset of the WorkOS attributes payload
	// from the user's directory_users row.
	Attributes UserAttributes
	// Groups are the names of the user's current directory groups.
	Groups []string
	// Roles are the user's current role slugs from the existing role tables.
	Roles []string
}

func (u UserInfo) AsAttributes() map[attr.Key]any {
	attrs := make(map[attr.Key]any, 5)
	if u.UserID != "" {
		attrs[attr.UserIDKey] = u.UserID
	}
	if u.Email != "" {
		attrs[attr.UserEmailKey] = u.Email
	}
	if !u.Attributes.IsZero() {
		attrs[attr.UserAttributesKey] = u.Attributes
	}
	if len(u.Groups) > 0 {
		attrs[attr.UserGroupsKey] = u.Groups
	}
	if len(u.Roles) > 0 {
		attrs[attr.UserRolesKey] = u.Roles
	}
	return attrs
}

func (t ToolInfo) AsAttributes() map[attr.Key]any {
	attrs := map[attr.Key]any{
		attr.ToolURNKey:        t.URN,
		attr.NameKey:           t.Name,
		attr.ToolNameKey:       t.Name,
		attr.ProjectIDKey:      t.ProjectID,
		attr.OrganizationIDKey: t.OrganizationID,
	}

	parsedURN, err := urn.ParseTool(t.URN)
	if err == nil {
		attrs[attr.ToolCallSourceKey] = parsedURN.Source
	}

	if t.DeploymentID != "" {
		attrs[attr.DeploymentIDKey] = t.DeploymentID
	}

	if t.FunctionID != nil {
		attrs[attr.FunctionIDKey] = *t.FunctionID
	}

	return attrs
}
