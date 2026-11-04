-- Set comment to table: "admin_mcp_authorization_grants"
COMMENT ON TABLE "admin_mcp_authorization_grants" IS '@mirror: skip';
-- Set comment to table: "admin_mcp_connections"
COMMENT ON TABLE "admin_mcp_connections" IS '@mirror: skip';
-- Set comment to table: "admin_mcp_oauth_clients"
COMMENT ON TABLE "admin_mcp_oauth_clients" IS '@mirror: skip';
-- Set comment to table: "admin_mcp_sessions"
COMMENT ON TABLE "admin_mcp_sessions" IS '@mirror: skip';
-- Set comment to table: "admin_mcp_write_events"
COMMENT ON TABLE "admin_mcp_write_events" IS '@mirror: skip';
-- Set comment to table: "admin_mcp_write_proposals"
COMMENT ON TABLE "admin_mcp_write_proposals" IS '@mirror: skip';
-- Set comment to table: "agent_connection_blocks"
COMMENT ON TABLE "agent_connection_blocks" IS 'Agents blocked by a connection owner from that owner''s connection to one upstream OAuth client, kept beside the mcp:blocked_auth grant that enforces it.
@mirror: copy';
-- Set comment to table: "agent_executions"
COMMENT ON TABLE "agent_executions" IS '@mirror: skip';
-- Set comment to table: "agent_role_assignments"
COMMENT ON TABLE "agent_role_assignments" IS '@mirror: copy';
-- Set comment to table: "agents"
COMMENT ON TABLE "agents" IS '@mirror: copy';
-- Set comment to table: "ai_integration_config_chats"
COMMENT ON TABLE "ai_integration_config_chats" IS '@mirror: skip';
-- Set comment to table: "ai_integration_configs"
COMMENT ON TABLE "ai_integration_configs" IS '@mirror: copy';
-- Set comment to column: "api_key_encrypted" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."api_key_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "ai_integration_syncs"
COMMENT ON TABLE "ai_integration_syncs" IS '@mirror: skip';
-- Set comment to table: "ai_product_cimd_documents"
COMMENT ON TABLE "ai_product_cimd_documents" IS 'CIMD documents each catalog product publishes. Every enabled live row admits its client_id on presets-mode issuers.
@mirror: match(url)';
-- Set comment to table: "ai_products"
COMMENT ON TABLE "ai_products" IS 'Global AI client catalog of products Speakeasy knows, curated by staff. Shadow AI scan targets and CIMD documents reference it.
@mirror: match(id)';
-- Set comment to table: "ai_scan_targets"
COMMENT ON TABLE "ai_scan_targets" IS '@mirror: copy';
-- Set comment to table: "api_keys"
COMMENT ON TABLE "api_keys" IS '@mirror: regen';
-- Set comment to table: "assets"
COMMENT ON TABLE "assets" IS '@mirror: copy';
-- Set comment to table: "assistant_agent_bindings"
COMMENT ON TABLE "assistant_agent_bindings" IS '@mirror: copy';
-- Set comment to table: "assistant_dashboard_messages"
COMMENT ON TABLE "assistant_dashboard_messages" IS '@mirror: skip';
-- Set comment to table: "assistant_inference_credentials"
COMMENT ON TABLE "assistant_inference_credentials" IS '@mirror: regen';
-- Set comment to table: "assistant_kit_gateway_credentials"
COMMENT ON TABLE "assistant_kit_gateway_credentials" IS '@mirror: regen';
-- Set comment to table: "assistant_kit_interrupt_requests"
COMMENT ON TABLE "assistant_kit_interrupt_requests" IS '@mirror: skip';
-- Set comment to table: "assistant_kit_sessions"
COMMENT ON TABLE "assistant_kit_sessions" IS '@mirror: skip';
-- Set comment to table: "assistant_mcp_oauth_clients"
COMMENT ON TABLE "assistant_mcp_oauth_clients" IS '@mirror: copy';
-- Set comment to column: "client_secret_encrypted" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."client_secret_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "assistant_mcp_servers"
COMMENT ON TABLE "assistant_mcp_servers" IS '@mirror: copy';
-- Set comment to table: "assistant_memories"
COMMENT ON TABLE "assistant_memories" IS '@mirror: skip';
-- Set comment to table: "assistant_runtimes"
COMMENT ON TABLE "assistant_runtimes" IS '@mirror: copy';
-- Set comment to table: "assistant_thread_events"
COMMENT ON TABLE "assistant_thread_events" IS '@mirror: skip';
-- Set comment to table: "assistant_threads"
COMMENT ON TABLE "assistant_threads" IS '@mirror: skip';
-- Set comment to table: "assistants"
COMMENT ON TABLE "assistants" IS '@mirror: copy';
-- Set comment to table: "audit_logs"
COMMENT ON TABLE "audit_logs" IS '@mirror: skip';
-- Set comment to table: "auth_permission_provisions"
COMMENT ON TABLE "auth_permission_provisions" IS 'Default auth permission grants provisioned by the backfill or at organization creation, one row per grant, kept after the grant itself is deleted.
@mirror: copy';
-- Set comment to table: "authz_challenge_resolutions"
COMMENT ON TABLE "authz_challenge_resolutions" IS 'Tracks admin resolutions of authz challenge denials. challenge_id references authz_challenges.id in ClickHouse (soft cross-DB reference).
@mirror: skip';
-- Set comment to table: "aws_iam_credentials"
COMMENT ON TABLE "aws_iam_credentials" IS '@mirror: regen';
-- Set comment to table: "aws_kms_keys"
COMMENT ON TABLE "aws_kms_keys" IS '@mirror: regen';
-- Set comment to table: "billing_cycle_usage"
COMMENT ON TABLE "billing_cycle_usage" IS '@mirror: skip';
-- Set comment to table: "billing_metadata"
COMMENT ON TABLE "billing_metadata" IS '@mirror: skip';
-- Set comment to table: "business_memories"
COMMENT ON TABLE "business_memories" IS '@mirror: skip';
-- Set comment to table: "chat_analysis_evaluations"
COMMENT ON TABLE "chat_analysis_evaluations" IS '@mirror: skip';
-- Set comment to table: "chat_analysis_settings"
COMMENT ON TABLE "chat_analysis_settings" IS '@mirror: skip';
-- Set comment to table: "chat_content_parts"
COMMENT ON TABLE "chat_content_parts" IS '@mirror: skip';
-- Set comment to table: "chat_message_participants"
COMMENT ON TABLE "chat_message_participants" IS '@mirror: skip';
-- Set comment to table: "chat_messages"
COMMENT ON TABLE "chat_messages" IS '@mirror: skip';
-- Set comment to table: "chat_resolution_messages"
COMMENT ON TABLE "chat_resolution_messages" IS '@mirror: skip';
-- Set comment to table: "chat_resolutions"
COMMENT ON TABLE "chat_resolutions" IS '@mirror: skip';
-- Set comment to table: "chat_session_links"
COMMENT ON TABLE "chat_session_links" IS '@mirror: skip';
-- Set comment to table: "chat_user_feedback"
COMMENT ON TABLE "chat_user_feedback" IS '@mirror: skip';
-- Set comment to table: "chats"
COMMENT ON TABLE "chats" IS '@mirror: skip';
-- Set comment to table: "custom_domains"
COMMENT ON TABLE "custom_domains" IS '@mirror: skip';
-- Set comment to table: "dashboard_widgets"
COMMENT ON TABLE "dashboard_widgets" IS '@mirror: copy';
-- Set comment to table: "dashboards"
COMMENT ON TABLE "dashboards" IS '@mirror: copy';
-- Set comment to table: "data_export_routes"
COMMENT ON TABLE "data_export_routes" IS '@mirror: copy';
-- Set comment to table: "deployment_logs"
COMMENT ON TABLE "deployment_logs" IS '@mirror: skip';
-- Set comment to table: "deployment_statuses"
COMMENT ON TABLE "deployment_statuses" IS '@mirror: copy';
-- Set comment to column: "deployment_id" on table: "deployment_statuses"
COMMENT ON COLUMN "deployment_statuses"."deployment_id" IS '@ref: deployments.id
@access: confidential';
-- Set comment to table: "deployment_tag_history"
COMMENT ON TABLE "deployment_tag_history" IS '@mirror: copy';
-- Set comment to table: "deployment_tags"
COMMENT ON TABLE "deployment_tags" IS '@mirror: copy';
-- Set comment to table: "deployments"
COMMENT ON TABLE "deployments" IS '@mirror: copy';
-- Set comment to table: "deployments_functions"
COMMENT ON TABLE "deployments_functions" IS '@mirror: copy';
-- Set comment to table: "deployments_openapiv3_assets"
COMMENT ON TABLE "deployments_openapiv3_assets" IS '@mirror: copy';
-- Set comment to table: "deployments_packages"
COMMENT ON TABLE "deployments_packages" IS '@mirror: copy';
-- Set comment to table: "device_agent_configurations"
COMMENT ON TABLE "device_agent_configurations" IS '@mirror: copy';
-- Set comment to table: "device_agent_device_syncs"
COMMENT ON TABLE "device_agent_device_syncs" IS '@mirror: skip';
-- Set comment to table: "device_agent_environment_syncs"
COMMENT ON TABLE "device_agent_environment_syncs" IS '@mirror: skip';
-- Set comment to table: "device_agent_state_reports"
COMMENT ON TABLE "device_agent_state_reports" IS '@mirror: copy';
-- Set comment to table: "device_agent_syncs"
COMMENT ON TABLE "device_agent_syncs" IS '@mirror: skip';
-- Set comment to table: "device_integration_configs"
COMMENT ON TABLE "device_integration_configs" IS '@mirror: copy';
-- Set comment to column: "credentials_encrypted" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."credentials_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "device_integration_schedules"
COMMENT ON TABLE "device_integration_schedules" IS '@mirror: copy';
-- Set comment to table: "device_integration_syncs"
COMMENT ON TABLE "device_integration_syncs" IS '@mirror: skip';
-- Set comment to table: "device_owners"
COMMENT ON TABLE "device_owners" IS '@mirror: copy';
-- Set comment to table: "directory_groups"
COMMENT ON TABLE "directory_groups" IS '@mirror: copy';
-- Set comment to table: "directory_review_attribute_opt_outs"
COMMENT ON TABLE "directory_review_attribute_opt_outs" IS '@mirror: copy';
-- Set comment to table: "directory_review_items"
COMMENT ON TABLE "directory_review_items" IS '@mirror: skip';
-- Set comment to table: "directory_review_org_states"
COMMENT ON TABLE "directory_review_org_states" IS '@mirror: copy';
-- Set comment to table: "directory_role_mappings"
COMMENT ON TABLE "directory_role_mappings" IS '@mirror: copy';
-- Set comment to table: "directory_user_group_memberships"
COMMENT ON TABLE "directory_user_group_memberships" IS '@mirror: copy';
-- Set comment to table: "directory_users"
COMMENT ON TABLE "directory_users" IS '@mirror: copy';
-- Set comment to table: "environment_entries"
COMMENT ON TABLE "environment_entries" IS '@mirror: copy';
-- Set comment to column: "value" on table: "environment_entries"
COMMENT ON COLUMN "environment_entries"."value" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "environments"
COMMENT ON TABLE "environments" IS '@mirror: copy';
-- Set comment to table: "execution_selection_profile_versions"
COMMENT ON TABLE "execution_selection_profile_versions" IS '@mirror: skip';
-- Set comment to table: "execution_selection_profiles"
COMMENT ON TABLE "execution_selection_profiles" IS '@mirror: skip';
-- Set comment to table: "external_credentials"
COMMENT ON TABLE "external_credentials" IS '@mirror: copy';
-- Set comment to table: "external_keys"
COMMENT ON TABLE "external_keys" IS '@mirror: regen';
-- Set comment to table: "external_mcp_attachments"
COMMENT ON TABLE "external_mcp_attachments" IS '@mirror: copy';
-- Set comment to table: "external_mcp_tool_definitions"
COMMENT ON TABLE "external_mcp_tool_definitions" IS '@mirror: copy';
-- Set comment to table: "external_oauth_client_registrations"
COMMENT ON TABLE "external_oauth_client_registrations" IS '@mirror: copy';
-- Set comment to column: "client_secret_encrypted" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."client_secret_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "external_oauth_server_metadata"
COMMENT ON TABLE "external_oauth_server_metadata" IS '@mirror: copy';
-- Set comment to table: "fly_apps"
COMMENT ON TABLE "fly_apps" IS '@mirror: skip';
-- Set comment to table: "function_resource_definitions"
COMMENT ON TABLE "function_resource_definitions" IS '@mirror: copy';
-- Set comment to table: "function_tool_definitions"
COMMENT ON TABLE "function_tool_definitions" IS '@mirror: copy';
-- Set comment to table: "functions_access"
COMMENT ON TABLE "functions_access" IS '@mirror: regen';
-- Set comment to table: "gcp_iam_credentials"
COMMENT ON TABLE "gcp_iam_credentials" IS '@mirror: regen';
-- Set comment to table: "gcp_kms_keys"
COMMENT ON TABLE "gcp_kms_keys" IS '@mirror: regen';
-- Set comment to table: "global_roles"
COMMENT ON TABLE "global_roles" IS '@mirror: match(workos_slug)';
-- Set comment to table: "hooks_server_name_overrides"
COMMENT ON TABLE "hooks_server_name_overrides" IS '@mirror: copy';
-- Set comment to table: "http_security"
COMMENT ON TABLE "http_security" IS '@mirror: copy';
-- Set comment to table: "http_tool_definitions"
COMMENT ON TABLE "http_tool_definitions" IS '@mirror: copy';
-- Set comment to table: "identity_provider_connections"
COMMENT ON TABLE "identity_provider_connections" IS '@mirror: copy';
-- Set comment to table: "internal_session_issuers"
COMMENT ON TABLE "internal_session_issuers" IS '@mirror: skip';
-- Set comment to table: "issuance_selection_profiles"
COMMENT ON TABLE "issuance_selection_profiles" IS '@mirror: copy';
-- Set comment to table: "json_web_key_sets"
COMMENT ON TABLE "json_web_key_sets" IS '@mirror: regen';
-- Set comment to table: "json_web_keys"
COMMENT ON TABLE "json_web_keys" IS '@mirror: regen';
-- Set comment to table: "killswitch_expiry_events"
COMMENT ON TABLE "killswitch_expiry_events" IS '@mirror: skip';
-- Set comment to table: "killswitch_operations"
COMMENT ON TABLE "killswitch_operations" IS '@mirror: copy';
-- Set comment to table: "killswitch_prescription_version_resources"
COMMENT ON TABLE "killswitch_prescription_version_resources" IS '@mirror: copy';
-- Set comment to table: "killswitch_prescription_versions"
COMMENT ON TABLE "killswitch_prescription_versions" IS '@mirror: copy';
-- Set comment to table: "killswitch_prescriptions"
COMMENT ON TABLE "killswitch_prescriptions" IS '@mirror: copy';
-- Set comment to table: "litellm_instances"
COMMENT ON TABLE "litellm_instances" IS '@mirror: regen';
-- Set comment to table: "mcp_approval_decisions"
COMMENT ON TABLE "mcp_approval_decisions" IS 'Append-only approve/deny history with the rationale and the evidence it rested on.
@mirror: skip';
-- Set comment to table: "mcp_approval_request_requesters"
COMMENT ON TABLE "mcp_approval_request_requesters" IS 'Who asked for a server and why. Separate from the request so demand is visible without duplicating reviews.
@mirror: skip';
-- Set comment to table: "mcp_approval_requests"
COMMENT ON TABLE "mcp_approval_requests" IS 'One review per MCP server per project. Re-requests reopen the same row so decisions accumulate as history, giving "have we decided on this before?" for free.
@mirror: skip';
-- Set comment to table: "mcp_auth_client_emas"
COMMENT ON TABLE "mcp_auth_client_emas" IS '@mirror: copy';
-- Set comment to table: "mcp_auth_client_headers"
COMMENT ON TABLE "mcp_auth_client_headers" IS '@mirror: copy';
-- Set comment to column: "value" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."value" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "mcp_auth_clients"
COMMENT ON TABLE "mcp_auth_clients" IS '@mirror: copy scope=project>org>global';
-- Set comment to table: "mcp_endpoints"
COMMENT ON TABLE "mcp_endpoints" IS '@mirror: copy';
-- Set comment to table: "mcp_environment_configs"
COMMENT ON TABLE "mcp_environment_configs" IS '@mirror: copy';
-- Set comment to table: "mcp_metadata"
COMMENT ON TABLE "mcp_metadata" IS '@mirror: copy';
-- Set comment to table: "mcp_registries"
COMMENT ON TABLE "mcp_registries" IS '@mirror: match(url)';
-- Set comment to table: "mcp_registry_entries"
COMMENT ON TABLE "mcp_registry_entries" IS '@mirror: match(id)';
-- Set comment to table: "mcp_research_reports"
COMMENT ON TABLE "mcp_research_reports" IS 'Research-agent output for an approval request. Findings are gathered and cited, never adjudicated — the admin decides.
@mirror: skip';
-- Set comment to table: "mcp_server_auth_clients"
COMMENT ON TABLE "mcp_server_auth_clients" IS '@mirror: copy';
-- Set comment to table: "mcp_server_remote_session_clients"
COMMENT ON TABLE "mcp_server_remote_session_clients" IS '@mirror: copy';
-- Set comment to table: "mcp_server_tool_metadata"
COMMENT ON TABLE "mcp_server_tool_metadata" IS '@mirror: copy';
-- Set comment to table: "mcp_servers"
COMMENT ON TABLE "mcp_servers" IS '@mirror: copy';
-- Set comment to column: "visibility" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."visibility" IS '@mirror: set(private) when=public
@access: confidential';
-- Set comment to table: "mcp_skill_links"
COMMENT ON TABLE "mcp_skill_links" IS '@mirror: copy';
-- Set comment to table: "mdm_devices"
COMMENT ON TABLE "mdm_devices" IS '@mirror: copy';
-- Set comment to table: "meta_mcp_server_members"
COMMENT ON TABLE "meta_mcp_server_members" IS '@mirror: copy';
-- Set comment to table: "meta_mcp_servers"
COMMENT ON TABLE "meta_mcp_servers" IS '@mirror: copy';
-- Set comment to table: "model_provider_keys"
COMMENT ON TABLE "model_provider_keys" IS '@mirror: copy';
-- Set comment to column: "api_key_encrypted" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."api_key_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "network_ingresses"
COMMENT ON TABLE "network_ingresses" IS '@mirror: copy';
-- Set comment to column: "credentials_encrypted" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."credentials_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "oauth_proxy_client_info"
COMMENT ON TABLE "oauth_proxy_client_info" IS '@mirror: copy';
-- Set comment to column: "mcp_slug" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."mcp_slug" IS '@ref: toolsets.mcp_slug
@access: confidential';
-- Set comment to column: "client_secret" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."client_secret" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "oauth_proxy_providers"
COMMENT ON TABLE "oauth_proxy_providers" IS '@mirror: copy';
-- Set comment to column: "secrets" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."secrets" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "oauth_proxy_servers"
COMMENT ON TABLE "oauth_proxy_servers" IS '@mirror: copy';
-- Set comment to table: "okta_application_assignments"
COMMENT ON TABLE "okta_application_assignments" IS '@mirror: skip';
-- Set comment to table: "okta_application_reconcile_runs"
COMMENT ON TABLE "okta_application_reconcile_runs" IS '@mirror: skip';
-- Set comment to table: "okta_applications"
COMMENT ON TABLE "okta_applications" IS '@mirror: copy';
-- Set comment to table: "okta_identity_provider_connections"
COMMENT ON TABLE "okta_identity_provider_connections" IS '@mirror: copy';
-- Set comment to table: "okta_resource_connections"
COMMENT ON TABLE "okta_resource_connections" IS '@mirror: copy';
-- Set comment to table: "okta_server_suggestion_dismissals"
COMMENT ON TABLE "okta_server_suggestion_dismissals" IS '@mirror: copy';
-- Set comment to table: "onboarding_playbook_steps"
COMMENT ON TABLE "onboarding_playbook_steps" IS '@mirror: copy';
-- Set comment to table: "onboarding_playbooks"
COMMENT ON TABLE "onboarding_playbooks" IS '@mirror: copy';
-- Set comment to table: "onboarding_step_dependencies"
COMMENT ON TABLE "onboarding_step_dependencies" IS '@mirror: match(id)';
-- Set comment to table: "onboarding_step_methods"
COMMENT ON TABLE "onboarding_step_methods" IS '@mirror: match(id)';
-- Set comment to table: "onboarding_steps"
COMMENT ON TABLE "onboarding_steps" IS 'Onboarding steps mirrored from application code; deleted_at marks a step the code no longer defines.
@mirror: match(slug)';
-- Set comment to table: "onboarding_use_cases"
COMMENT ON TABLE "onboarding_use_cases" IS '@mirror: match(slug)';
-- Set comment to table: "openrouter_api_keys"
COMMENT ON TABLE "openrouter_api_keys" IS '@mirror: regen';
-- Set comment to table: "openrouter_spend_daily"
COMMENT ON TABLE "openrouter_spend_daily" IS '@mirror: skip';
-- Set comment to table: "organization_features"
COMMENT ON TABLE "organization_features" IS '@mirror: copy';
-- Set comment to table: "organization_invitations"
COMMENT ON TABLE "organization_invitations" IS '@mirror: skip';
-- Set comment to table: "organization_metadata"
COMMENT ON TABLE "organization_metadata" IS '@mirror: copy';
-- Set comment to column: "default_host" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."default_host" IS '@mirror: rewrite(host)
@access: confidential';
-- Set comment to table: "organization_onboarding"
COMMENT ON TABLE "organization_onboarding" IS '@mirror: copy';
-- Set comment to table: "organization_onboarding_vendors"
COMMENT ON TABLE "organization_onboarding_vendors" IS '@mirror: copy';
-- Set comment to table: "organization_promotion_applications"
COMMENT ON TABLE "organization_promotion_applications" IS '@mirror: copy';
-- Set comment to table: "organization_role_assignments"
COMMENT ON TABLE "organization_role_assignments" IS '@mirror: copy';
-- Set comment to table: "organization_roles"
COMMENT ON TABLE "organization_roles" IS '@mirror: copy';
-- Set comment to table: "organization_setup_tasks"
COMMENT ON TABLE "organization_setup_tasks" IS '@mirror: copy';
-- Set comment to table: "organization_user_relationships"
COMMENT ON TABLE "organization_user_relationships" IS '@mirror: copy';
-- Set comment to table: "otel_destinations"
COMMENT ON TABLE "otel_destinations" IS '@mirror: copy';
-- Set comment to column: "headers_encrypted" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."headers_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "otel_forwarding_configs"
COMMENT ON TABLE "otel_forwarding_configs" IS '@mirror: copy';
-- Set comment to column: "headers_encrypted" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."headers_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "outbox"
COMMENT ON TABLE "outbox" IS '@mirror: skip';
-- Set comment to table: "outbox_relays"
COMMENT ON TABLE "outbox_relays" IS '@mirror: skip';
-- Set comment to table: "package_versions"
COMMENT ON TABLE "package_versions" IS '@mirror: copy';
-- Set comment to table: "packages"
COMMENT ON TABLE "packages" IS '@mirror: copy';
-- Set comment to table: "platform_mcp_authorization_grants"
COMMENT ON TABLE "platform_mcp_authorization_grants" IS '@mirror: skip';
-- Set comment to table: "platform_mcp_catalog_registrations"
COMMENT ON TABLE "platform_mcp_catalog_registrations" IS '@mirror: copy';
-- Set comment to table: "platform_mcp_connections"
COMMENT ON TABLE "platform_mcp_connections" IS '@mirror: skip';
-- Set comment to table: "platform_mcp_distributions"
COMMENT ON TABLE "platform_mcp_distributions" IS '@mirror: copy';
-- Set comment to table: "platform_mcp_feedback"
COMMENT ON TABLE "platform_mcp_feedback" IS '@mirror: skip';
-- Set comment to table: "platform_mcp_oauth_clients"
COMMENT ON TABLE "platform_mcp_oauth_clients" IS '@mirror: match(client_id)';
-- Set comment to table: "platform_mcp_onboarding_milestones"
COMMENT ON TABLE "platform_mcp_onboarding_milestones" IS '@mirror: copy';
-- Set comment to table: "platform_mcp_onboarding_workflows"
COMMENT ON TABLE "platform_mcp_onboarding_workflows" IS '@mirror: copy';
-- Set comment to table: "platform_mcp_operation_receipts"
COMMENT ON TABLE "platform_mcp_operation_receipts" IS '@mirror: skip';
-- Set comment to table: "platform_mcp_readiness"
COMMENT ON TABLE "platform_mcp_readiness" IS '@mirror: copy';
-- Set comment to table: "platform_mcp_selected_use_evidence"
COMMENT ON TABLE "platform_mcp_selected_use_evidence" IS '@mirror: skip';
-- Set comment to table: "platform_mcp_sessions"
COMMENT ON TABLE "platform_mcp_sessions" IS '@mirror: skip';
-- Set comment to table: "platform_mcp_setup_handoffs"
COMMENT ON TABLE "platform_mcp_setup_handoffs" IS '@mirror: skip';
-- Set comment to table: "plugin_assignments"
COMMENT ON TABLE "plugin_assignments" IS '@mirror: copy';
-- Set comment to table: "plugin_github_connections"
COMMENT ON TABLE "plugin_github_connections" IS '@mirror: copy';
-- Set comment to column: "repo_owner" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."repo_owner" IS '@mirror: set(mirror.invalid)
@access: confidential-pii';
-- Set comment to column: "marketplace_token" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."marketplace_token" IS '@mirror: redact
@access: secret-restricted';
-- Set comment to table: "plugin_servers"
COMMENT ON TABLE "plugin_servers" IS '@mirror: copy';
-- Set comment to table: "plugins"
COMMENT ON TABLE "plugins" IS '@mirror: copy';
-- Set comment to table: "principal_grants"
COMMENT ON TABLE "principal_grants" IS 'RBAC grants. Normalized: one row per (org, principal, scope). Selectors can further constrain applicability.
@mirror: copy';
-- Set comment to table: "principal_remote_session_bindings"
COMMENT ON TABLE "principal_remote_session_bindings" IS '@mirror: copy';
-- Set comment to table: "project_allowed_origins"
COMMENT ON TABLE "project_allowed_origins" IS '@mirror: copy';
-- Set comment to table: "project_managed_assistants"
COMMENT ON TABLE "project_managed_assistants" IS '@mirror: copy';
-- Set comment to table: "project_marketplace_settings"
COMMENT ON TABLE "project_marketplace_settings" IS '@mirror: copy';
-- Set comment to table: "project_tool_variations"
COMMENT ON TABLE "project_tool_variations" IS '@mirror: copy';
-- Set comment to table: "projects"
COMMENT ON TABLE "projects" IS '@mirror: copy';
-- Set comment to table: "prompt_templates"
COMMENT ON TABLE "prompt_templates" IS '@mirror: copy';
-- Set comment to table: "publish_outbox"
COMMENT ON TABLE "publish_outbox" IS 'Transactional outbox of pending Pub/Sub publishes. Rows are deleted once published, so the table is near-empty in steady state; permanent failures move to publish_outbox_dead_letters.
@mirror: skip';
-- Set comment to table: "publish_outbox_dead_letters"
COMMENT ON TABLE "publish_outbox_dead_letters" IS '@mirror: skip';
-- Set comment to table: "registry_promotion_candidates"
COMMENT ON TABLE "registry_promotion_candidates" IS '@mirror: copy';
-- Set comment to table: "remote_mcp_server_headers"
COMMENT ON TABLE "remote_mcp_server_headers" IS '@mirror: copy';
-- Set comment to column: "value" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."value" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "remote_mcp_servers"
COMMENT ON TABLE "remote_mcp_servers" IS '@mirror: copy';
-- Set comment to table: "remote_protected_resources"
COMMENT ON TABLE "remote_protected_resources" IS '@mirror: copy';
-- Set comment to table: "remote_session_client_user_session_issuers"
COMMENT ON TABLE "remote_session_client_user_session_issuers" IS '@mirror: copy';
-- Set comment to table: "remote_session_clients"
COMMENT ON TABLE "remote_session_clients" IS '@mirror: copy scope=project>org>global';
-- Set comment to column: "client_secret_encrypted" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_secret_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "remote_session_ema_bindings"
COMMENT ON TABLE "remote_session_ema_bindings" IS '@mirror: copy';
-- Set comment to table: "remote_session_ema_credentials"
COMMENT ON TABLE "remote_session_ema_credentials" IS '@mirror: skip';
-- Set comment to table: "remote_session_ema_providers"
COMMENT ON TABLE "remote_session_ema_providers" IS '@mirror: copy';
-- Set comment to table: "remote_session_issuers"
COMMENT ON TABLE "remote_session_issuers" IS '@mirror: copy scope=project>org';
-- Set comment to table: "remote_sessions"
COMMENT ON TABLE "remote_sessions" IS '@mirror: copy';
-- Set comment to column: "access_token_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."access_token_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to column: "refresh_token_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."refresh_token_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to column: "auto_refresh" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."auto_refresh" IS '@mirror: set(false)
@access: confidential';
-- Set comment to table: "risk_custom_detection_rules"
COMMENT ON TABLE "risk_custom_detection_rules" IS '@mirror: copy';
-- Set comment to table: "risk_exclusions"
COMMENT ON TABLE "risk_exclusions" IS '@mirror: copy';
-- Set comment to column: "match_value" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."match_value" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "risk_execution_evidence"
COMMENT ON TABLE "risk_execution_evidence" IS '@mirror: skip';
-- Set comment to table: "risk_finding_evidence"
COMMENT ON TABLE "risk_finding_evidence" IS '@mirror: skip';
-- Set comment to table: "risk_policies"
COMMENT ON TABLE "risk_policies" IS '@mirror: copy';
-- Set comment to table: "risk_policy_approval_messages"
COMMENT ON TABLE "risk_policy_approval_messages" IS 'Notifications sent for a Speakeasy Approvals request, such as one Slack DM per approver.
@mirror: copy';
-- Set comment to table: "risk_policy_approvals"
COMMENT ON TABLE "risk_policy_approvals" IS 'Speakeasy Approvals: a tool call held by a require_approval risk policy, from block through request, decision and consumption.
@mirror: skip';
-- Set comment to table: "risk_policy_bypass_requests"
COMMENT ON TABLE "risk_policy_bypass_requests" IS 'Risk-policy bypass request workflow. A block records a request here; an admin approves by granting risk_policy:bypass.
@mirror: copy';
-- Set comment to table: "risk_policy_challenges"
COMMENT ON TABLE "risk_policy_challenges" IS 'Interactive warn/challenge lifecycle for warn-action policies: a warn match records a challenged row; the user self-service acknowledges to proceed on retry. Never stores the raw matched value.
@mirror: copy';
-- Set comment to table: "risk_policy_eval_reviews"
COMMENT ON TABLE "risk_policy_eval_reviews" IS '@mirror: skip';
-- Set comment to table: "risk_results"
COMMENT ON TABLE "risk_results" IS '@mirror: skip';
-- Set comment to table: "security_audit_logs"
COMMENT ON TABLE "security_audit_logs" IS '@mirror: skip';
-- Set comment to table: "session_handoff_links"
COMMENT ON TABLE "session_handoff_links" IS '@mirror: skip';
-- Set comment to table: "session_quarantines"
COMMENT ON TABLE "session_quarantines" IS '@mirror: copy';
-- Set comment to table: "sigint_custom_signals"
COMMENT ON TABLE "sigint_custom_signals" IS '@mirror: copy';
-- Set comment to table: "sigint_definition_snapshots"
COMMENT ON TABLE "sigint_definition_snapshots" IS '@mirror: copy';
-- Set comment to table: "sigint_sensor_signals"
COMMENT ON TABLE "sigint_sensor_signals" IS '@mirror: copy';
-- Set comment to table: "sigint_sensors"
COMMENT ON TABLE "sigint_sensors" IS '@mirror: copy';
-- Set comment to table: "sign_in_revocation_cutoffs"
COMMENT ON TABLE "sign_in_revocation_cutoffs" IS '@mirror: copy';
-- Set comment to table: "sign_in_revocation_deliveries"
COMMENT ON TABLE "sign_in_revocation_deliveries" IS '@mirror: skip';
-- Set comment to table: "sign_ins"
COMMENT ON TABLE "sign_ins" IS '@mirror: skip';
-- Set comment to table: "signup_offers"
COMMENT ON TABLE "signup_offers" IS '@mirror: match(stripe_promotion_code_id)';
-- Set comment to table: "skill_distributions"
COMMENT ON TABLE "skill_distributions" IS '@mirror: copy';
-- Set comment to table: "skill_edit_suggestion_changes"
COMMENT ON TABLE "skill_edit_suggestion_changes" IS '@mirror: skip';
-- Set comment to table: "skill_edit_suggestion_feedback"
COMMENT ON TABLE "skill_edit_suggestion_feedback" IS '@mirror: skip';
-- Set comment to table: "skill_edit_suggestions"
COMMENT ON TABLE "skill_edit_suggestions" IS '@mirror: skip';
-- Set comment to table: "skill_efficacy_evaluations"
COMMENT ON TABLE "skill_efficacy_evaluations" IS '@mirror: copy';
-- Set comment to table: "skill_efficacy_settings"
COMMENT ON TABLE "skill_efficacy_settings" IS '@mirror: copy';
-- Set comment to table: "skill_feedback"
COMMENT ON TABLE "skill_feedback" IS '@mirror: skip';
-- Set comment to table: "skill_observations"
COMMENT ON TABLE "skill_observations" IS '@mirror: skip';
-- Set comment to table: "skill_raw_hashes"
COMMENT ON TABLE "skill_raw_hashes" IS '@mirror: copy';
-- Set comment to table: "skill_share_links"
COMMENT ON TABLE "skill_share_links" IS '@mirror: skip';
-- Set comment to table: "skill_sync_receipts"
COMMENT ON TABLE "skill_sync_receipts" IS '@mirror: skip';
-- Set comment to table: "skill_version_lineages"
COMMENT ON TABLE "skill_version_lineages" IS '@mirror: copy';
-- Set comment to table: "skill_version_origins"
COMMENT ON TABLE "skill_version_origins" IS '@mirror: copy';
-- Set comment to table: "skill_versions"
COMMENT ON TABLE "skill_versions" IS '@mirror: copy';
-- Set comment to table: "skills"
COMMENT ON TABLE "skills" IS '@mirror: copy';
-- Set comment to table: "slack_app_toolsets"
COMMENT ON TABLE "slack_app_toolsets" IS '@mirror: copy';
-- Set comment to table: "slack_apps"
COMMENT ON TABLE "slack_apps" IS '@mirror: copy';
-- Set comment to column: "slack_client_secret" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_client_secret" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to column: "slack_signing_secret" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_signing_secret" IS '@mirror: redact
@access: secret-restricted';
-- Set comment to column: "slack_bot_token" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_bot_token" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "slack_directory_connections"
COMMENT ON TABLE "slack_directory_connections" IS '@mirror: copy';
-- Set comment to column: "credentials_encrypted" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."credentials_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "slack_directory_memberships"
COMMENT ON TABLE "slack_directory_memberships" IS '@mirror: copy';
-- Set comment to table: "slack_identity_mappings"
COMMENT ON TABLE "slack_identity_mappings" IS '@mirror: copy';
-- Set comment to table: "slack_registrations"
COMMENT ON TABLE "slack_registrations" IS '@mirror: copy';
-- Set comment to table: "source_environments"
COMMENT ON TABLE "source_environments" IS '@mirror: copy';
-- Set comment to table: "spend_rule_events"
COMMENT ON TABLE "spend_rule_events" IS '@mirror: skip';
-- Set comment to table: "spend_rules"
COMMENT ON TABLE "spend_rules" IS '@mirror: copy';
-- Set comment to column: "limit_usd_cents" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."limit_usd_cents" IS '@mirror: keep reason="spend-rule limit is org configuration; needed to reproduce policy decisions"
@access: restricted';
-- Set comment to table: "stripe_invoice_allocations"
COMMENT ON TABLE "stripe_invoice_allocations" IS '@mirror: skip';
-- Set comment to table: "stripe_invoices"
COMMENT ON TABLE "stripe_invoices" IS '@mirror: skip';
-- Set comment to table: "stripe_meter_reports"
COMMENT ON TABLE "stripe_meter_reports" IS '@mirror: skip';
-- Set comment to table: "stripe_webhook_receipts"
COMMENT ON TABLE "stripe_webhook_receipts" IS '@mirror: skip';
-- Set comment to table: "support_matrix_capabilities"
COMMENT ON TABLE "support_matrix_capabilities" IS 'Individual capabilities grouped by category; categories are not blanket support claims.
@mirror: match(slug)';
-- Set comment to table: "support_matrix_coverage"
COMMENT ON TABLE "support_matrix_coverage" IS 'Explicit method-platform-capability coverage. Missing rows are unknown; applicability must also be established before claiming support.
@mirror: match(method_platform_id, capability_id)';
-- Set comment to table: "support_matrix_integration_methods"
COMMENT ON TABLE "support_matrix_integration_methods" IS 'Integration methods available for assessing support; plan_notes preserve method-level eligibility claims.
@mirror: match(slug)';
-- Set comment to table: "support_matrix_method_capabilities"
COMMENT ON TABLE "support_matrix_method_capabilities" IS 'Method-level reference claims. These do not establish support for any specific platform.
@mirror: match(integration_method_id, capability_id)';
-- Set comment to table: "support_matrix_method_platforms"
COMMENT ON TABLE "support_matrix_method_platforms" IS 'Applicability of a method to a platform, assessed separately from its capability coverage. Missing rows are unknown.
@mirror: match(integration_method_id, platform_id)';
-- Set comment to table: "support_matrix_plans"
COMMENT ON TABLE "support_matrix_plans" IS 'Plans each vendor sells, as the support matrix names them; an organization declares the one it is on per vendor.
@mirror: match(slug)';
-- Set comment to table: "support_matrix_platforms"
COMMENT ON TABLE "support_matrix_platforms" IS 'Global admin support catalog of upstream product surfaces, independent of customer installations.
@mirror: match(slug)';
-- Set comment to table: "tool_call_blocks"
COMMENT ON TABLE "tool_call_blocks" IS 'Durable record of a blocked tool call or prompt. One row per hook-time block decision, carrying the exact reason shown to the agent. Backs the durable /blocks/:id page and its thumbs feedback. The risk_results / risk_policies foreign keys are nullable enrichment links — the page renders from this row alone.
@mirror: copy';
-- Set comment to table: "tool_variations"
COMMENT ON TABLE "tool_variations" IS '@mirror: copy';
-- Set comment to table: "tool_variations_groups"
COMMENT ON TABLE "tool_variations_groups" IS '@mirror: copy';
-- Set comment to table: "toolset_embeddings"
COMMENT ON TABLE "toolset_embeddings" IS '@mirror: skip';
-- Set comment to table: "toolset_environments"
COMMENT ON TABLE "toolset_environments" IS '@mirror: copy';
-- Set comment to table: "toolset_origins"
COMMENT ON TABLE "toolset_origins" IS '@mirror: copy';
-- Set comment to table: "toolset_prompts"
COMMENT ON TABLE "toolset_prompts" IS '@mirror: copy';
-- Set comment to table: "toolset_versions"
COMMENT ON TABLE "toolset_versions" IS '@mirror: copy';
-- Set comment to table: "toolsets"
COMMENT ON TABLE "toolsets" IS '@mirror: copy';
-- Set comment to column: "mcp_is_public" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."mcp_is_public" IS '@mirror: set(false)
@access: confidential';
-- Set comment to table: "trials"
COMMENT ON TABLE "trials" IS '@mirror: skip';
-- Set comment to table: "trigger_instances"
COMMENT ON TABLE "trigger_instances" IS '@mirror: skip';
-- Set comment to table: "trigger_thread_routes"
COMMENT ON TABLE "trigger_thread_routes" IS '@mirror: skip';
-- Set comment to table: "trigger_workload_bindings"
COMMENT ON TABLE "trigger_workload_bindings" IS '@mirror: copy';
-- Set comment to table: "trusted_issuer_sessions"
COMMENT ON TABLE "trusted_issuer_sessions" IS '@mirror: copy';
-- Set comment to column: "identity_assertion_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."identity_assertion_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to column: "refresh_token_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."refresh_token_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to column: "offline_access_request_config_hash" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."offline_access_request_config_hash" IS '@mirror: keep reason="non-secret configuration fingerprint"
@access: secret-restricted';
-- Set comment to column: "upstream_subject_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."upstream_subject_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to column: "nonce_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."nonce_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to column: "credential_config_hash" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."credential_config_hash" IS '@mirror: keep reason="non-secret configuration fingerprint"
@access: secret-restricted';
-- Set comment to table: "tunneled_mcp_server_headers"
COMMENT ON TABLE "tunneled_mcp_server_headers" IS '@mirror: skip';
-- Set comment to table: "tunneled_mcp_servers"
COMMENT ON TABLE "tunneled_mcp_servers" IS 'Customer-hosted MCP server sources that connect to Speakeasy through outbound tunnels.
@mirror: skip';
-- Set comment to table: "unproxied_mcp_servers"
COMMENT ON TABLE "unproxied_mcp_servers" IS '@mirror: copy';
-- Set comment to table: "upstream_client_selections"
COMMENT ON TABLE "upstream_client_selections" IS '@mirror: copy';
-- Set comment to table: "user_accounts"
COMMENT ON TABLE "user_accounts" IS '@mirror: copy';
-- Set comment to table: "user_hidden_mcp_servers"
COMMENT ON TABLE "user_hidden_mcp_servers" IS '@mirror: copy';
-- Set comment to table: "user_oauth_tokens"
COMMENT ON TABLE "user_oauth_tokens" IS '@mirror: copy';
-- Set comment to column: "access_token_encrypted" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."access_token_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to column: "refresh_token_encrypted" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."refresh_token_encrypted" IS '@mirror: placeholder
@access: secret-restricted';
-- Set comment to table: "user_session_clients"
COMMENT ON TABLE "user_session_clients" IS '@mirror: copy';
-- Set comment to column: "client_secret_hash" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_secret_hash" IS '@mirror: redact
@access: secret-restricted';
-- Set comment to table: "user_session_consent_resources"
COMMENT ON TABLE "user_session_consent_resources" IS '@mirror: copy';
-- Set comment to table: "user_session_consents"
COMMENT ON TABLE "user_session_consents" IS '@mirror: copy';
-- Set comment to table: "user_session_issuer_cimd_clients"
COMMENT ON TABLE "user_session_issuer_cimd_clients" IS '@mirror: copy';
-- Set comment to table: "user_session_issuers"
COMMENT ON TABLE "user_session_issuers" IS '@mirror: copy scope=project>org>global';
-- Set comment to table: "user_sessions"
COMMENT ON TABLE "user_sessions" IS '@mirror: skip';
-- Set comment to table: "users"
COMMENT ON TABLE "users" IS '@mirror: regen';
-- Set comment to table: "widgets"
COMMENT ON TABLE "widgets" IS '@mirror: copy';
-- Set comment to table: "workload_agent_assignments"
COMMENT ON TABLE "workload_agent_assignments" IS '@mirror: copy';
-- Set comment to table: "workload_device_join_tokens"
COMMENT ON TABLE "workload_device_join_tokens" IS '@mirror: regen';
-- Set comment to table: "workload_device_keys"
COMMENT ON TABLE "workload_device_keys" IS '@mirror: regen';
-- Set comment to table: "workload_identity_admissions"
COMMENT ON TABLE "workload_identity_admissions" IS '@mirror: copy';
-- Set comment to table: "workload_issuers"
COMMENT ON TABLE "workload_issuers" IS '@mirror: copy';
-- Set comment to table: "workos_organization_syncs"
COMMENT ON TABLE "workos_organization_syncs" IS '@mirror: skip';
-- Set comment to table: "workos_user_syncs"
COMMENT ON TABLE "workos_user_syncs" IS '@mirror: skip';
