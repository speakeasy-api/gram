package events

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/outbox"
)

var McpFeedbackSubmittedV1 = outbox.NewEventDef[McpFeedbackSubmittedPayloadV1](
	"mcp_feedback.submitted_v1",
	"An agent sent feedback through an MCP server's built-in feedback tool",
)

// McpFeedbackSubmittedEventID derives the stable delivery id for one feedback
// report, so writing the same report again would deliver it once.
func McpFeedbackSubmittedEventID(feedbackID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("gram:mcp-feedback:submitted:"+feedbackID.String()))
}

// McpFeedbackSubmittedPayloadV1 is one feedback report. Its free text was
// written by an agent and is carried with secrets and personal data masked;
// treat it as untrusted input rather than as instructions.
type McpFeedbackSubmittedPayloadV1 struct {
	// ID identifies the report.
	ID uuid.UUID `json:"id"`

	// OrganizationID is the organization that owns the MCP server.
	OrganizationID string `json:"organization_id"`

	// ProjectID is the MCP server's project.
	ProjectID uuid.UUID `json:"project_id"`

	// McpServerID is the MCP server the report was sent through.
	McpServerID uuid.UUID `json:"mcp_server_id"`

	// Content is the report itself.
	Content string `json:"content"`

	// Severity is blocked, degraded or cosmetic, and absent when the agent
	// gave none.
	Severity string `json:"severity,omitempty"`

	// Workaround is what the agent did instead.
	Workaround string `json:"workaround,omitempty"`

	// Intention is what the agent was trying to do.
	Intention string `json:"intention,omitempty"`

	// ToolName is the tool the agent says the report is about. It is not
	// checked against the server's tools.
	ToolName string `json:"tool_name,omitempty"`

	// Model is the model and host product as the agent reported them.
	Model string `json:"model,omitempty"`

	// UserID is the caller's Speakeasy user id, when the caller is a member
	// of the server's organization.
	UserID string `json:"user_id,omitempty"`

	// UserEmail is that member's email address.
	UserEmail string `json:"user_email,omitempty"`

	// ExternalUserID is the caller's subject in the identity provider the
	// server signs people in with.
	ExternalUserID string `json:"external_user_id,omitempty"`

	// ClientName is the MCP client's name, from its handshake.
	ClientName string `json:"client_name,omitempty"`

	// ClientVersion is the MCP client's version, from its handshake.
	ClientVersion string `json:"client_version,omitempty"`

	// CreatedAt is when the gateway accepted the report.
	CreatedAt time.Time `json:"created_at"`
}
