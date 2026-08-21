import { assertNever } from "@/lib/utils";

/**
 * Every audit action the server can record, mirroring the `Action` constants in
 * `server/internal/audit/*.go`. Listing them here (rather than treating
 * `log.action` as an opaque string) is what lets `staticActionPhrase` be an
 * exhaustive switch: adding a Go action without a phrase here fails type-check
 * the moment the string lands in this list.
 */
export const AUDIT_ACTIONS = [
  "access_challenge:resolve",
  "access_member:update_role",
  "access_role:create",
  "access_role:delete",
  "access_role:update",
  "ai_integration:delete",
  "ai_integration:retry_schedule",
  "ai_integration:update_schedule",
  "ai_integration:upsert",
  "api_key:create",
  "api_key:revoke",
  "asset:create",
  "assistant:tool_call",
  "aws_iam_credential:create",
  "aws_iam_credential:delete",
  "aws_iam_credential:update",
  "aws_kms_key:create",
  "aws_kms_key:delete",
  "aws_kms_key:update",
  "billing_metadata:cancel_stripe_subscription",
  "billing_metadata:create_stripe_checkout",
  "billing_metadata:create_stripe_portal",
  "billing_metadata:resume_stripe_subscription",
  "billing_metadata:update",
  "chat_analysis_settings:upsert",
  "chat_session:access",
  "chat_session:handoff_export",
  "chat_session:move",
  "custom_domains:create",
  "custom_domains:delete",
  "custom_domains:update",
  "deployments:create",
  "deployments:evolve",
  "deployments:redeploy",
  "device_integration:delete",
  "device_integration:retry_schedule",
  "device_integration:update_schedule",
  "device_integration:upsert",
  "environment:create",
  "environment:delete",
  "environment:update",
  "gcp_iam_credential:create",
  "gcp_iam_credential:delete",
  "gcp_iam_credential:update",
  "gcp_kms_key:create",
  "gcp_kms_key:delete",
  "gcp_kms_key:update",
  "litellm_instance:create",
  "litellm_instance:revoke",
  "litellm_instance:rotate_key",
  "mcp-endpoint:create",
  "mcp-endpoint:delete",
  "mcp-endpoint:update",
  "mcp-server:create",
  "mcp-server:delete",
  "mcp-server:update",
  "mcp-server:update-tool-metadata",
  "mcp_approval_request:approve",
  "mcp_approval_request:create",
  "mcp_approval_request:deny",
  "mcp_approval_request:evidence_changed",
  "mcp_approval_request:research_start",
  "mcp_collection:attach_server",
  "mcp_collection:create",
  "mcp_collection:delete",
  "mcp_collection:detach_server",
  "mcp_collection:update",
  "mcp_metadata:update",
  "model_provider_key:delete",
  "model_provider_key:upsert",
  "openrouter-key:disable",
  "openrouter-key:enable",
  "openrouter-key:set_spend_cap",
  "organization:device_agent_configuration_updated",
  "organization:enterprise_trial_armed",
  "organization:enterprise_trial_demoted",
  "organization:enterprise_trial_extended",
  "organization:enterprise_trial_rearmed",
  "organization:hooks_fail_open_disabled",
  "organization:hooks_fail_open_enabled",
  "organization:payg_activated",
  "organization:payg_deactivated",
  "organization:webhooks_disabled",
  "organization:webhooks_enabled",
  "organization_invitation:create",
  "organization_invitation:revoke",
  "organization_invitation:update_role",
  "otel_forwarding:delete",
  "otel_forwarding:upsert",
  "platform-mcp-registration:create",
  "platform-mcp-registration:handoff_issue",
  "platform-mcp-registration:handoff_redeem",
  "plugin:assignments_set",
  "plugin:create",
  "plugin:delete",
  "plugin:publish",
  "plugin:server_add",
  "plugin:server_remove",
  "plugin:server_update",
  "plugin:update",
  "project:create",
  "project:delete",
  "project:update",
  "remote-mcp-server-header:create",
  "remote-mcp-server-header:delete",
  "remote-mcp-server-header:update",
  "remote-mcp:create",
  "remote-mcp:delete",
  "remote-mcp:update",
  "remote-session-client:attach-user-session-issuer",
  "remote-session-client:create",
  "remote-session-client:delete",
  "remote-session-client:detach-mcp-server",
  "remote-session-client:detach-user-session-issuer",
  "remote-session-client:revoke-sessions",
  "remote-session-client:update",
  "remote-session-issuer:create",
  "remote-session-issuer:delete",
  "remote-session-issuer:migrate",
  "remote-session-issuer:update",
  "remote-session:delete",
  "remote-session:refresh",
  "risk_exclusion:create",
  "risk_exclusion:delete",
  "risk_exclusion:update",
  "risk_policy:bypass_request_approve",
  "risk_policy:bypass_request_create",
  "risk_policy:bypass_request_deny",
  "risk_policy:bypass_request_revoke",
  "risk_policy:challenge_acknowledge",
  "risk_policy:create",
  "risk_policy:delete",
  "risk_policy:eval_review_delete",
  "risk_policy:eval_review_save",
  "risk_policy:trigger",
  "risk_policy:update",
  "risk_result:dismiss",
  "risk_result:restore",
  "risk_result:unmask",
  "skill:add_version",
  "skill:archive",
  "skill:create",
  "skill:distribute",
  "skill:restore_version",
  "skill:share_link_create",
  "skill:share_link_revoke",
  "skill:suggestion_approve",
  "skill:suggestion_dismiss",
  "skill:undistribute",
  "skill:update",
  "skill:update_distribution",
  "skill_efficacy_settings:upsert",
  "spend_rule:archive",
  "spend_rule:create",
  "spend_rule:update",
  "template:create",
  "template:delete",
  "template:update",
  "toolset:attach_external_oauth",
  "toolset:attach_oauth_proxy",
  "toolset:create",
  "toolset:delete",
  "toolset:detach_external_oauth",
  "toolset:detach_oauth_proxy",
  "toolset:update",
  "toolset:update_oauth_proxy",
  "trigger-instance:create",
  "trigger-instance:delete",
  "trigger-instance:pause",
  "trigger-instance:resume",
  "trigger-instance:update",
  "tunneled-mcp:create",
  "tunneled-mcp:delete",
  "tunneled-mcp:dynamic-client-registration-attempt",
  "tunneled-mcp:rotate-key",
  "tunneled-mcp:update",
  "unproxied-mcp:create",
  "unproxied-mcp:delete",
  "user-session-client:cimd-refresh",
  "user-session-client:revoke",
  "user-session-consent:revoke",
  "user-session-issuer-cimd-client:add",
  "user-session-issuer-cimd-client:remove",
  "user-session-issuer:create",
  "user-session-issuer:delete",
  "user-session-issuer:update",
  "user-session:revoke",
  "variation:delete_global",
  "variation:update_global",
  "wake:cancelled",
  "wake:fired",
  "wake:scheduled",
] as const;

export type AuditAction = (typeof AUDIT_ACTIONS)[number];

const AUDIT_ACTION_SET: ReadonlySet<string> = new Set(AUDIT_ACTIONS);

export function isAuditAction(action: string): action is AuditAction {
  return AUDIT_ACTION_SET.has(action);
}

/**
 * Past-tense phrase for an action, written to read as a sentence with the actor
 * in front and the subject display name behind it:
 *
 *   "adam@example.com" + "deleted risk policy" + "Shadow MCP Server Policy"
 *
 * Phrases that dangle a preposition ("... invite for") expect a subject name;
 * the rest read fine either way.
 */
export function staticActionPhrase(action: AuditAction): string {
  switch (action) {
    case "access_challenge:resolve":
      return "resolved access challenge";
    case "access_member:update_role":
      return "changed member role for";
    case "access_role:create":
      return "created access role";
    case "access_role:update":
      return "updated access role";
    case "access_role:delete":
      return "deleted access role";

    case "ai_integration:upsert":
      return "configured AI integration";
    case "ai_integration:delete":
      return "removed AI integration";
    case "ai_integration:update_schedule":
      return "updated AI integration schedule";
    case "ai_integration:retry_schedule":
      return "retried AI integration sync";

    case "api_key:create":
      return "created API key";
    case "api_key:revoke":
      return "revoked API key";

    case "asset:create":
      return "uploaded asset";
    case "assistant:tool_call":
      return "ran assistant tool";

    case "aws_iam_credential:create":
      return "added AWS IAM credential";
    case "aws_iam_credential:update":
      return "updated AWS IAM credential";
    case "aws_iam_credential:delete":
      return "removed AWS IAM credential";
    case "aws_kms_key:create":
      return "added AWS KMS key";
    case "aws_kms_key:update":
      return "updated AWS KMS key";
    case "aws_kms_key:delete":
      return "removed AWS KMS key";
    case "gcp_iam_credential:create":
      return "added GCP IAM credential";
    case "gcp_iam_credential:update":
      return "updated GCP IAM credential";
    case "gcp_iam_credential:delete":
      return "removed GCP IAM credential";
    case "gcp_kms_key:create":
      return "added GCP KMS key";
    case "gcp_kms_key:update":
      return "updated GCP KMS key";
    case "gcp_kms_key:delete":
      return "removed GCP KMS key";

    case "billing_metadata:create_stripe_checkout":
      return "started Stripe checkout for";
    case "billing_metadata:create_stripe_portal":
      return "opened Stripe billing portal for";
    case "billing_metadata:cancel_stripe_subscription":
      return "canceled Stripe subscription for";
    case "billing_metadata:resume_stripe_subscription":
      return "resumed Stripe subscription for";
    case "billing_metadata:update":
      return "updated billing metadata";
    case "chat_analysis_settings:upsert":
      return "updated chat analysis settings";
    case "chat_session:access":
      return "opened chat session";
    case "chat_session:handoff_export":
      return "exported chat session handoff";
    case "chat_session:move":
      return "moved chat session";

    case "custom_domains:create":
      return "added custom domain";
    case "custom_domains:update":
      return "updated custom domain";
    case "custom_domains:delete":
      return "removed custom domain";

    case "deployments:create":
      return "created deployment";
    case "deployments:evolve":
      return "updated deployment";
    case "deployments:redeploy":
      return "redeployed deployment";

    case "device_integration:upsert":
      return "configured device integration";
    case "device_integration:delete":
      return "removed device integration";
    case "device_integration:update_schedule":
      return "updated device integration schedule";
    case "device_integration:retry_schedule":
      return "retried device integration sync";

    case "environment:create":
      return "created environment";
    case "environment:update":
      return "updated environment";
    case "environment:delete":
      return "deleted environment";

    case "litellm_instance:create":
      return "created LiteLLM instance";
    case "litellm_instance:rotate_key":
      return "rotated LiteLLM instance key";
    case "litellm_instance:revoke":
      return "revoked LiteLLM instance";

    case "mcp-endpoint:create":
      return "created MCP endpoint";
    case "mcp-endpoint:update":
      return "updated MCP endpoint";
    case "mcp-endpoint:delete":
      return "deleted MCP endpoint";

    case "mcp-server:create":
      return "created MCP server";
    case "mcp-server:update":
      return "updated MCP server";
    case "mcp-server:delete":
      return "deleted MCP server";
    case "mcp-server:update-tool-metadata":
      return "updated tool metadata on MCP server";

    case "mcp_approval_request:approve":
      return "approved MCP access to";
    case "mcp_approval_request:create":
      return "requested MCP access to";
    case "mcp_approval_request:deny":
      return "denied MCP access to";
    case "mcp_approval_request:evidence_changed":
      return "detected changed evidence for approved MCP server";
    case "mcp_approval_request:research_start":
      return "started research on";

    case "mcp_collection:create":
      return "created collection";
    case "mcp_collection:update":
      return "updated collection";
    case "mcp_collection:delete":
      return "deleted collection";
    case "mcp_collection:attach_server":
      return "added a server to collection";
    case "mcp_collection:detach_server":
      return "removed a server from collection";

    case "mcp_metadata:update":
      return "updated MCP metadata for";

    case "model_provider_key:upsert":
      return "updated model provider key";
    case "model_provider_key:delete":
      return "removed model provider key";

    case "openrouter-key:disable":
      return "disabled platform OpenRouter key";
    case "openrouter-key:enable":
      return "enabled platform OpenRouter key";
    case "openrouter-key:set_spend_cap":
      return "changed inference cap for";

    case "organization:webhooks_enabled":
      return "enabled webhook delivery";
    case "organization:webhooks_disabled":
      return "disabled webhook delivery";
    case "organization:hooks_fail_open_enabled":
      return "enabled fail-open for hooks";
    case "organization:hooks_fail_open_disabled":
      return "disabled fail-open for hooks";
    case "organization:device_agent_configuration_updated":
      return "updated device agent configuration";
    case "organization:enterprise_trial_armed":
      return "started enterprise trial";
    case "organization:enterprise_trial_demoted":
      return "ended enterprise trial";
    case "organization:enterprise_trial_extended":
      return "extended enterprise trial";
    case "organization:enterprise_trial_rearmed":
      return "restarted enterprise trial";
    case "organization:payg_activated":
      return "activated pay-as-you-go billing for";
    case "organization:payg_deactivated":
      return "deactivated pay-as-you-go billing for";

    case "organization_invitation:create":
      return "invited";
    case "organization_invitation:revoke":
      return "revoked invite for";
    case "organization_invitation:update_role":
      return "changed invite role for";

    case "otel_forwarding:upsert":
      return "updated OpenTelemetry forwarding configuration";
    case "otel_forwarding:delete":
      return "removed OpenTelemetry forwarding configuration";

    case "platform-mcp-registration:create":
      return "registered platform MCP server";
    case "platform-mcp-registration:handoff_issue":
      return "issued a registration handoff for";
    case "platform-mcp-registration:handoff_redeem":
      return "redeemed a registration handoff for";

    case "plugin:create":
      return "created plugin";
    case "plugin:update":
      return "updated plugin";
    case "plugin:delete":
      return "deleted plugin";
    case "plugin:server_add":
      return "added a server to plugin";
    case "plugin:server_update":
      return "updated a server on plugin";
    case "plugin:server_remove":
      return "removed a server from plugin";
    case "plugin:assignments_set":
      return "updated plugin access";
    case "plugin:publish":
      return "published plugins";

    case "project:create":
      return "created project";
    case "project:update":
      return "updated project";
    case "project:delete":
      return "deleted project";

    case "remote-mcp:create":
      return "added remote MCP server";
    case "remote-mcp:update":
      return "updated remote MCP server";
    case "remote-mcp:delete":
      return "removed remote MCP server";
    case "remote-mcp-server-header:create":
      return "added a header to remote MCP server";
    case "remote-mcp-server-header:update":
      return "updated a header on remote MCP server";
    case "remote-mcp-server-header:delete":
      return "removed a header from remote MCP server";

    case "remote-session:refresh":
      return "refreshed remote session";
    case "remote-session:delete":
      return "deleted remote session";
    case "remote-session-client:create":
      return "created remote session client";
    case "remote-session-client:update":
      return "updated remote session client";
    case "remote-session-client:delete":
      return "deleted remote session client";
    case "remote-session-client:attach-user-session-issuer":
      return "attached a user session issuer to";
    case "remote-session-client:detach-user-session-issuer":
      return "detached a user session issuer from";
    case "remote-session-client:detach-mcp-server":
      return "detached an MCP server from";
    case "remote-session-client:revoke-sessions":
      return "revoked sessions for";
    case "remote-session-issuer:create":
      return "created remote session issuer";
    case "remote-session-issuer:update":
      return "updated remote session issuer";
    case "remote-session-issuer:delete":
      return "deleted remote session issuer";
    case "remote-session-issuer:migrate":
      return "migrated remote session issuer";

    case "risk_exclusion:create":
      return "created risk exclusion";
    case "risk_exclusion:update":
      return "updated risk exclusion";
    case "risk_exclusion:delete":
      return "deleted risk exclusion";

    case "risk_policy:create":
      return "created risk policy";
    case "risk_policy:update":
      return "updated risk policy";
    case "risk_policy:delete":
      return "deleted risk policy";
    case "risk_policy:trigger":
      return "triggered risk policy";
    case "risk_policy:bypass_request_create":
      return "requested a bypass for";
    case "risk_policy:bypass_request_approve":
      return "approved a bypass for";
    case "risk_policy:bypass_request_deny":
      return "denied a bypass for";
    case "risk_policy:bypass_request_revoke":
      return "revoked a bypass for";
    case "risk_policy:challenge_acknowledge":
      return "acknowledged a challenge for";
    case "risk_policy:eval_review_save":
      return "saved an evaluation review for";
    case "risk_policy:eval_review_delete":
      return "deleted an evaluation review for";

    case "risk_result:dismiss":
      return "dismissed risk finding";
    case "risk_result:restore":
      return "restored risk finding";
    case "risk_result:unmask":
      return "unmasked risk finding";

    case "skill:create":
      return "created skill";
    case "skill:update":
      return "updated skill";
    case "skill:archive":
      return "archived skill";
    case "skill:add_version":
      return "added a version to skill";
    case "skill:restore_version":
      return "restored a version of skill";
    case "skill:distribute":
      return "distributed skill";
    case "skill:undistribute":
      return "stopped distributing skill";
    case "skill:update_distribution":
      return "updated distribution for skill";
    case "skill:share_link_create":
      return "created a share link for";
    case "skill:share_link_revoke":
      return "revoked a share link for";
    case "skill:suggestion_approve":
      return "approved a suggestion for";
    case "skill:suggestion_dismiss":
      return "dismissed a suggestion for";
    case "skill_efficacy_settings:upsert":
      return "updated skill efficacy settings";

    case "spend_rule:create":
      return "created spend rule";
    case "spend_rule:update":
      return "updated spend rule";
    case "spend_rule:archive":
      return "archived spend rule";

    case "template:create":
      return "created template";
    case "template:update":
      return "updated template";
    case "template:delete":
      return "deleted template";

    case "toolset:create":
      return "created MCP server";
    case "toolset:update":
      return "updated MCP server";
    case "toolset:delete":
      return "deleted MCP server";
    case "toolset:attach_external_oauth":
      return "connected an external OAuth server to";
    case "toolset:detach_external_oauth":
      return "disconnected an external OAuth server from";
    case "toolset:attach_oauth_proxy":
      return "attached an OAuth proxy to";
    case "toolset:update_oauth_proxy":
      return "updated the OAuth proxy on";
    case "toolset:detach_oauth_proxy":
      return "detached an OAuth proxy from";

    case "trigger-instance:create":
      return "created trigger";
    case "trigger-instance:update":
      return "updated trigger";
    case "trigger-instance:delete":
      return "deleted trigger";
    case "trigger-instance:pause":
      return "paused trigger";
    case "trigger-instance:resume":
      return "resumed trigger";
    case "wake:scheduled":
      return "scheduled a wake for";
    case "wake:fired":
      return "ran a scheduled wake for";
    case "wake:cancelled":
      return "cancelled a wake for";

    case "tunneled-mcp:create":
      return "added tunneled MCP server";
    case "tunneled-mcp:update":
      return "updated tunneled MCP server";
    case "tunneled-mcp:delete":
      return "removed tunneled MCP server";
    case "tunneled-mcp:dynamic-client-registration-attempt":
      return "attempted OAuth client registration through tunneled MCP server";
    case "tunneled-mcp:rotate-key":
      return "rotated the key for tunneled MCP server";
    case "unproxied-mcp:create":
      return "added unproxied MCP server";
    case "unproxied-mcp:delete":
      return "removed unproxied MCP server";

    case "user-session:revoke":
      return "revoked user session";
    case "user-session-client:cimd-refresh":
      return "refreshed the client metadata document for";
    case "user-session-client:revoke":
      return "revoked user session client";
    case "user-session-consent:revoke":
      return "revoked consent for";
    case "user-session-issuer:create":
      return "created user session issuer";
    case "user-session-issuer:update":
      return "updated user session issuer";
    case "user-session-issuer:delete":
      return "deleted user session issuer";
    case "user-session-issuer-cimd-client:add":
      return "added a CIMD client to";
    case "user-session-issuer-cimd-client:remove":
      return "removed a CIMD client from";

    case "variation:update_global":
      return "updated a global variation for";
    case "variation:delete_global":
      return "deleted a global variation for";

    default:
      return assertNever(action);
  }
}
