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

// userAttributes is the v0 allowlist of directory attributes stamped onto
// telemetry rows: the struct fields are the allowlist. They are WorkOS
// predefined attributes (https://workos.com/docs/directory-sync/attributes):
// named and schematized by WorkOS, auto-mapped across directory providers, so
// they mean the same thing for every organization. Customer-defined custom
// attributes are deliberately excluded for now; Postgres keeps the full
// payload, so expanding this later only requires hydrating new rows.
type userAttributes struct {
	DepartmentName string `json:"department_name,omitempty"`
	JobTitle       string `json:"job_title,omitempty"`
	EmployeeType   string `json:"employee_type,omitempty"`
	DivisionName   string `json:"division_name,omitempty"`
	CostCenterName string `json:"cost_center_name,omitempty"`
}

func emptyUserAttributes() userAttributes {
	return userAttributes{
		DepartmentName: "",
		JobTitle:       "",
		EmployeeType:   "",
		DivisionName:   "",
		CostCenterName: "",
	}
}

func (a userAttributes) IsZero() bool {
	return a == emptyUserAttributes()
}

// UserInfo identifies the user a telemetry log row is attributed to. Callers
// provide either a Gram user ID or an email address; the logger resolves the
// other identity field and directory context during hydration.
type UserInfo struct {
	userID string
	email  string
}

func UserInfoByID(userID string) UserInfo {
	return UserInfo{userID: userID, email: ""}
}

func UserInfoByEmail(email string) UserInfo {
	return UserInfo{userID: "", email: email}
}

func UserInfoByIDAndEmail(userID, email string) UserInfo {
	return UserInfo{userID: userID, email: email}
}

func (u UserInfo) UserID() string {
	return u.userID
}

func (u UserInfo) Email() string {
	return u.email
}

func (u UserInfo) AsAttributes() map[attr.Key]any {
	attrs := make(map[attr.Key]any, 2)
	if u.userID != "" {
		attrs[attr.UserIDKey] = u.userID
	}
	if u.email != "" {
		attrs[attr.UserEmailKey] = u.email
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
