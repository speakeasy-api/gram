package events

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// AuditLogCreated is the event emitted after every audit log entry is written.
var AuditLogCreated = outbox.NewEventDef[AuditLogCreatedPayload](
	"audit_log.created",
	"An audit log entry was recorded",
)

var (
	AccessChallenge      = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.access_challenge_event_v1", "Emitted when changes to access challenges are made")
	AccessMember         = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.access_member_event_v1", "Emitted when changes to org members are made")
	AccessRole           = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.access_role_event_v1", "Emitted when changes to roles are made")
	APIKey               = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.api_key_event_v1", "Emitted when changes to API keys are made")
	Asset                = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.asset_event_v1", "Emitted when changes to assets are made")
	AssistantWake        = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.assistant_wake_event_v1", "Emitted when an assistant wake is scheduled or canceled")
	CustomDomain         = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.custom_domain_event_v1", "Emitted when changes to custom domains are made")
	Deployment           = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.deployment_event_v1", "Emitted when changes to deployments are made")
	Environment          = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.environment_event_v1", "Emitted when changes to environments are made")
	McpCollection        = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.mcp_collection_event_v1", "Emitted when changes to MCP collections are made")
	McpEndpoint          = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.mcp_endpoint_event_v1", "Emitted when changes to MCP endpoints are made")
	McpServer            = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.mcp_server_event_v1", "Emitted when changes to MCP servers are made")
	OrganizationInvite   = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.organization_invite_event_v1", "Emitted when changes to organization invites are made")
	OrganizationWebhooks = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.organization_webhooks_event_v1", "Emitted when changes to organization webhooks are made")
	OtelForwarding       = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.otel_forwarding_event_v1", "Emitted when changes to OTEL forwarding configs are made")
	Plugin               = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.plugin_event_v1", "Emitted when changes to plugins are made")
	Project              = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.project_event_v1", "Emitted when changes to projects are made")
	RemoteMcpServer      = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.remote_mcp_server_event_v1", "Emitted when changes to remote MCP servers are made")
	RemoteSessionClient  = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.remote_session_client_event_v1", "Emitted when changes to remote session clients are made")
	RemoteSession        = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.remote_session_event_v1", "Emitted when changes to remote sessions are made")
	RemoteSessionIssuer  = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.remote_session_issuer_event_v1", "Emitted when changes to remote session issuers are made")
	RiskPolicy           = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.risk_policy_event_v1", "Emitted when changes to risk policies are made")
	Template             = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.template_event_v1", "Emitted when changes to prompt templates are made")
	Toolset              = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.toolset_event_v1", "Emitted when changes to toolsets used by MCP servers are made")
	TriggerInstance      = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.trigger_instance_event_v1", "Emitted when changes to assistant triggers are made")
	UserSessionClient    = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.user_session_client_event_v1", "Emitted when changes to user session clients are made")
	UserSessionConsent   = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.user_session_consent_event_v1", "Emitted when changes to user session consents are made")
	UserSession          = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.user_session_event_v1", "Emitted when changes to user sessions are made")
	UserSessionIssuer    = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.user_session_issuer_event_v1", "Emitted when changes to user session issuers are made")
	Variation            = outbox.NewEventDef[AuditLogCreatedPayload]("audit_log.variation_event_v1", "Emitted when changes to tool names and other properties are made")
)

// AuditLogCreatedPayload is the webhook payload for audit_log.created events.
type AuditLogCreatedPayload struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID string    `json:"organization_id"`

	ActorID     string `json:"actor_id"`
	ActorType   string `json:"actor_type"`
	Action      string `json:"action"`
	SubjectID   string `json:"subject_id"`
	SubjectType string `json:"subject_type"`

	ProjectID          uuid.NullUUID   `json:"project_id,omitzero"`
	ActorDisplayName   string          `json:"actor_display_name,omitzero"`
	ActorSlug          string          `json:"actor_slug,omitzero"`
	SubjectDisplayName string          `json:"subject_display_name,omitzero"`
	SubjectSlug        string          `json:"subject_slug,omitzero"`
	BeforeSnapshot     json.RawMessage `json:"before_snapshot,omitempty"`
	AfterSnapshot      json.RawMessage `json:"after_snapshot,omitempty"`
	Metadata           json.RawMessage `json:"metadata,omitempty"`
}
