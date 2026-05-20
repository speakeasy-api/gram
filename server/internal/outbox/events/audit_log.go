package events

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// AuditLogCreated is the event emitted after every audit log entry is written.
//
// Deprecated: use the subject-scoped events like ProjectV1 and DeploymentV1
var AuditLogCreated = outbox.NewEventDef[AuditLogCreatedPayloadV1](
	"audit_log.created",
	"An audit log entry was recorded",
)

var (
	AccessChallengeV1      = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.access_challenge_event_v1", "Emitted when changes to access challenges are made")
	AccessMemberV1         = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.access_member_event_v1", "Emitted when changes to org members are made")
	AccessRoleV1           = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.access_role_event_v1", "Emitted when changes to roles are made")
	APIKeyV1               = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.api_key_event_v1", "Emitted when changes to API keys are made")
	AssetV1                = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.asset_event_v1", "Emitted when changes to assets are made")
	AssistantWakeV1        = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.assistant_wake_event_v1", "Emitted when an assistant wake is scheduled or canceled")
	CustomDomainV1         = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.custom_domain_event_v1", "Emitted when changes to custom domains are made")
	DeploymentV1           = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.deployment_event_v1", "Emitted when changes to deployments are made")
	EnvironmentV1          = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.environment_event_v1", "Emitted when changes to environments are made")
	McpCollectionV1        = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.mcp_collection_event_v1", "Emitted when changes to MCP collections are made")
	McpEndpointV1          = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.mcp_endpoint_event_v1", "Emitted when changes to MCP endpoints are made")
	McpServerV1            = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.mcp_server_event_v1", "Emitted when changes to MCP servers are made")
	OrganizationInviteV1   = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.organization_invite_event_v1", "Emitted when changes to organization invites are made")
	OrganizationWebhooksV1 = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.organization_webhooks_event_v1", "Emitted when changes to organization webhooks are made")
	OtelForwardingV1       = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.otel_forwarding_event_v1", "Emitted when changes to OTEL forwarding configs are made")
	PluginV1               = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.plugin_event_v1", "Emitted when changes to plugins are made")
	ProjectV1              = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.project_event_v1", "Emitted when changes to projects are made")
	RemoteMcpServerV1      = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.remote_mcp_server_event_v1", "Emitted when changes to remote MCP servers are made")
	RemoteSessionClientV1  = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.remote_session_client_event_v1", "Emitted when changes to remote session clients are made")
	RemoteSessionV1        = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.remote_session_event_v1", "Emitted when changes to remote sessions are made")
	RemoteSessionIssuerV1  = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.remote_session_issuer_event_v1", "Emitted when changes to remote session issuers are made")
	RiskPolicyV1           = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.risk_policy_event_v1", "Emitted when changes to risk policies are made")
	TemplateV1             = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.template_event_v1", "Emitted when changes to prompt templates are made")
	ToolsetV1              = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.toolset_event_v1", "Emitted when changes to toolsets used by MCP servers are made")
	TriggerInstanceV1      = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.trigger_instance_event_v1", "Emitted when changes to assistant triggers are made")
	UserSessionClientV1    = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.user_session_client_event_v1", "Emitted when changes to user session clients are made")
	UserSessionConsentV1   = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.user_session_consent_event_v1", "Emitted when changes to user session consents are made")
	UserSessionV1          = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.user_session_event_v1", "Emitted when changes to user sessions are made")
	UserSessionIssuerV1    = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.user_session_issuer_event_v1", "Emitted when changes to user session issuers are made")
	VariationV1            = outbox.NewEventDef[AuditLogCreatedPayloadV1]("audit_log.variation_event_v1", "Emitted when changes to tool names and other properties are made")
)

// AuditLogCreatedPayloadV1 is the webhook payload for audit_log.created events.
type AuditLogCreatedPayloadV1 struct {
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
