-- Set comment to column: "id" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."id" IS '@access: confidential';
-- Set comment to column: "authorization_code_hash" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."authorization_code_hash" IS '@access: secret-restricted';
-- Set comment to column: "oauth_client_id" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."connection_generation" IS '@access: confidential';
-- Set comment to column: "redirect_uri" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."redirect_uri" IS '@access: restricted';
-- Set comment to column: "code_challenge" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."code_challenge" IS '@access: secret-restricted';
-- Set comment to column: "scopes" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."scopes" IS '@access: confidential';
-- Set comment to column: "resource_uri" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."resource_uri" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."expires_at" IS '@access: confidential';
-- Set comment to column: "consumed_at" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."consumed_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "admin_mcp_authorization_grants"
COMMENT ON COLUMN "admin_mcp_authorization_grants"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."id" IS '@access: confidential';
-- Set comment to column: "subject_urn" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."subject_urn" IS '@access: confidential-pii';
-- Set comment to column: "oauth_client_id" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "admin_session_id_enc" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."admin_session_id_enc" IS '@access: secret-restricted';
-- Set comment to column: "scopes" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."scopes" IS '@access: confidential';
-- Set comment to column: "resource_uri" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."resource_uri" IS '@access: confidential';
-- Set comment to column: "active_generation" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."active_generation" IS '@access: confidential';
-- Set comment to column: "authorized_at" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."authorized_at" IS '@access: confidential';
-- Set comment to column: "reauthorized_at" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."reauthorized_at" IS '@access: confidential';
-- Set comment to column: "authorization_expires_at" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."authorization_expires_at" IS '@access: confidential';
-- Set comment to column: "reauthorization_required_at" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."reauthorization_required_at" IS '@access: confidential';
-- Set comment to column: "reauthorization_reason" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."reauthorization_reason" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."id" IS '@access: confidential';
-- Set comment to column: "client_id" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_id" IS '@access: confidential';
-- Set comment to column: "client_secret_hash" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_secret_hash" IS '@access: secret-restricted';
-- Set comment to column: "client_name" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_name" IS '@access: opaque-restricted';
-- Set comment to column: "redirect_uris" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."redirect_uris" IS '@access: opaque-restricted';
-- Set comment to column: "client_id_issued_at" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_id_issued_at" IS '@access: confidential';
-- Set comment to column: "client_secret_expires_at" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."revoked_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_uri" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_id_metadata_uri" IS '@access: restricted';
-- Set comment to column: "client_id_metadata_fetched_at" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_id_metadata_fetched_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_cache_expires_at" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_id_metadata_cache_expires_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_etag" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."client_id_metadata_etag" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "admin_mcp_oauth_clients"
COMMENT ON COLUMN "admin_mcp_oauth_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."connection_id" IS '@access: confidential';
-- Set comment to column: "oauth_client_id" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."connection_generation" IS '@access: confidential';
-- Set comment to column: "jti" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."jti" IS '@access: secret-restricted';
-- Set comment to column: "refresh_token_hash" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."refresh_token_hash" IS '@access: secret-restricted';
-- Set comment to column: "expires_at" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."expires_at" IS '@access: confidential';
-- Set comment to column: "refresh_expires_at" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."refresh_expires_at" IS '@access: confidential';
-- Set comment to column: "rotated_at" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."rotated_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."revoked_at" IS '@access: confidential';
-- Set comment to column: "replaced_by_session_id" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."replaced_by_session_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "admin_mcp_sessions"
COMMENT ON COLUMN "admin_mcp_sessions"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "agent_executions"
COMMENT ON COLUMN "agent_executions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "agent_executions"
COMMENT ON COLUMN "agent_executions"."project_id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "agent_executions"
COMMENT ON COLUMN "agent_executions"."deployment_id" IS '@access: confidential';
-- Set comment to column: "status" on table: "agent_executions"
COMMENT ON COLUMN "agent_executions"."status" IS '@access: confidential';
-- Set comment to column: "started_at" on table: "agent_executions"
COMMENT ON COLUMN "agent_executions"."started_at" IS '@access: confidential';
-- Set comment to column: "completed_at" on table: "agent_executions"
COMMENT ON COLUMN "agent_executions"."completed_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "agent_executions"
COMMENT ON COLUMN "agent_executions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "agent_executions"
COMMENT ON COLUMN "agent_executions"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "agent_role_assignments"
COMMENT ON COLUMN "agent_role_assignments"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "agent_role_assignments"
COMMENT ON COLUMN "agent_role_assignments"."organization_id" IS '@access: confidential';
-- Set comment to column: "agent_id" on table: "agent_role_assignments"
COMMENT ON COLUMN "agent_role_assignments"."agent_id" IS '@access: confidential';
-- Set comment to column: "role_urn" on table: "agent_role_assignments"
COMMENT ON COLUMN "agent_role_assignments"."role_urn" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "agent_role_assignments"
COMMENT ON COLUMN "agent_role_assignments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "agent_role_assignments"
COMMENT ON COLUMN "agent_role_assignments"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "agent_role_assignments"
COMMENT ON COLUMN "agent_role_assignments"."deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "agents"
COMMENT ON COLUMN "agents"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "agents"
COMMENT ON COLUMN "agents"."organization_id" IS '@access: confidential';
-- Set comment to column: "owner_user_id" on table: "agents"
COMMENT ON COLUMN "agents"."owner_user_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "agents"
COMMENT ON COLUMN "agents"."name" IS '@access: confidential';
-- Set comment to column: "suspended_at" on table: "agents"
COMMENT ON COLUMN "agents"."suspended_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "agents"
COMMENT ON COLUMN "agents"."revoked_at" IS '@access: confidential';
-- Set comment to column: "owner_reassignment_required_at" on table: "agents"
COMMENT ON COLUMN "agents"."owner_reassignment_required_at" IS '@access: confidential';
-- Set comment to column: "owner_reassignment_reason" on table: "agents"
COMMENT ON COLUMN "agents"."owner_reassignment_reason" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "agents"
COMMENT ON COLUMN "agents"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "agents"
COMMENT ON COLUMN "agents"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "agents"
COMMENT ON COLUMN "agents"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "agents"
COMMENT ON COLUMN "agents"."deleted" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "agents"
COMMENT ON COLUMN "agents"."project_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "ai_integration_config_chats"
COMMENT ON COLUMN "ai_integration_config_chats"."id" IS '@access: confidential';
-- Set comment to column: "ai_integration_config_id" on table: "ai_integration_config_chats"
COMMENT ON COLUMN "ai_integration_config_chats"."ai_integration_config_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "ai_integration_config_chats"
COMMENT ON COLUMN "ai_integration_config_chats"."chat_id" IS '@access: confidential';
-- Set comment to column: "last_cursor_id" on table: "ai_integration_config_chats"
COMMENT ON COLUMN "ai_integration_config_chats"."last_cursor_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "ai_integration_config_chats"
COMMENT ON COLUMN "ai_integration_config_chats"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ai_integration_config_chats"
COMMENT ON COLUMN "ai_integration_config_chats"."updated_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."created_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."deleted_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."updated_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."organization_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."provider" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."project_id" IS '@access: confidential';
-- Set comment to column: "api_key_encrypted" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."api_key_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "enabled" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."enabled" IS '@access: confidential';
-- Set comment to column: "id" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."id" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."deleted" IS '@access: confidential';
-- Set comment to column: "external_organization_id" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."external_organization_id" IS '@access: restricted';
-- Set comment to column: "billing_mode" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."billing_mode" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."updated_at" IS '@access: confidential';
-- Set comment to column: "ai_integration_config_id" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."ai_integration_config_id" IS '@access: confidential';
-- Set comment to column: "poll_watermark_at" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."poll_watermark_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."id" IS '@access: confidential';
-- Set comment to column: "next_poll_after" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."next_poll_after" IS '@access: confidential';
-- Set comment to column: "last_poll_error" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."last_poll_error" IS '@access: opaque-restricted';
-- Set comment to column: "last_poll_failed_at" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."last_poll_failed_at" IS '@access: confidential';
-- Set comment to column: "last_poll_success_at" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."last_poll_success_at" IS '@access: confidential';
-- Set comment to column: "consecutive_failures" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."consecutive_failures" IS '@access: confidential';
-- Set comment to column: "last_cursor_id" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."last_cursor_id" IS '@access: confidential';
-- Set comment to column: "schedule" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."schedule" IS '@access: confidential';
-- Set comment to column: "kind" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."kind" IS '@access: confidential';
-- Set comment to column: "poll_checkpoint" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."poll_checkpoint" IS '@access: opaque-restricted';
-- Set comment to column: "auto_paused_at" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."auto_paused_at" IS '@access: confidential';
-- Set comment to column: "disabled_at" on table: "ai_integration_syncs"
COMMENT ON COLUMN "ai_integration_syncs"."disabled_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."organization_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."id" IS '@access: confidential';
-- Set comment to column: "display_name" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."display_name" IS '@access: confidential';
-- Set comment to column: "category" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."category" IS '@access: confidential';
-- Set comment to column: "bundle_ids" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."bundle_ids" IS '@access: confidential';
-- Set comment to column: "binaries" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."binaries" IS '@access: confidential';
-- Set comment to column: "config_dirs" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."config_dirs" IS '@access: opaque-restricted';
-- Set comment to column: "process_names" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."process_names" IS '@access: confidential';
-- Set comment to column: "version_plist_key" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."version_plist_key" IS '@access: confidential';
-- Set comment to column: "cimd_vendor_keys" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."cimd_vendor_keys" IS '@access: confidential';
-- Set comment to column: "oauth_client_ids" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."oauth_client_ids" IS '@access: restricted';
-- Set comment to column: "client_info_names" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."client_info_names" IS '@access: confidential';
-- Set comment to column: "status" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."status" IS '@access: confidential';
-- Set comment to column: "rationale" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."rationale" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ai_scan_targets"
COMMENT ON COLUMN "ai_scan_targets"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."project_id" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."name" IS '@access: confidential';
-- Set comment to column: "scopes" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."scopes" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."deleted" IS '@access: confidential';
-- Set comment to column: "key_prefix" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."key_prefix" IS '@access: secret-restricted';
-- Set comment to column: "key_hash" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."key_hash" IS '@access: secret-restricted';
-- Set comment to column: "last_accessed_at" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."last_accessed_at" IS '@access: confidential';
-- Set comment to column: "subject_urn" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."subject_urn" IS '@access: confidential';
-- Set comment to column: "delegated_grants" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."delegated_grants" IS '@access: opaque-restricted';
-- Set comment to column: "delegated_grants_version" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."delegated_grants_version" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."expires_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "assets"
COMMENT ON COLUMN "assets"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assets"
COMMENT ON COLUMN "assets"."project_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "assets"
COMMENT ON COLUMN "assets"."name" IS '@access: confidential';
-- Set comment to column: "url" on table: "assets"
COMMENT ON COLUMN "assets"."url" IS '@access: confidential';
-- Set comment to column: "kind" on table: "assets"
COMMENT ON COLUMN "assets"."kind" IS '@access: confidential';
-- Set comment to column: "content_type" on table: "assets"
COMMENT ON COLUMN "assets"."content_type" IS '@access: confidential';
-- Set comment to column: "content_length" on table: "assets"
COMMENT ON COLUMN "assets"."content_length" IS '@access: confidential';
-- Set comment to column: "sha256" on table: "assets"
COMMENT ON COLUMN "assets"."sha256" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assets"
COMMENT ON COLUMN "assets"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assets"
COMMENT ON COLUMN "assets"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "assets"
COMMENT ON COLUMN "assets"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "assets"
COMMENT ON COLUMN "assets"."deleted" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "assets"
COMMENT ON COLUMN "assets"."organization_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "assistant_dashboard_messages"
COMMENT ON COLUMN "assistant_dashboard_messages"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_dashboard_messages"
COMMENT ON COLUMN "assistant_dashboard_messages"."project_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "assistant_dashboard_messages"
COMMENT ON COLUMN "assistant_dashboard_messages"."chat_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "assistant_dashboard_messages"
COMMENT ON COLUMN "assistant_dashboard_messages"."user_id" IS '@access: confidential';
-- Set comment to column: "role" on table: "assistant_dashboard_messages"
COMMENT ON COLUMN "assistant_dashboard_messages"."role" IS '@access: confidential';
-- Set comment to column: "content" on table: "assistant_dashboard_messages"
COMMENT ON COLUMN "assistant_dashboard_messages"."content" IS '@access: opaque-restricted';
-- Set comment to column: "seq" on table: "assistant_dashboard_messages"
COMMENT ON COLUMN "assistant_dashboard_messages"."seq" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_dashboard_messages"
COMMENT ON COLUMN "assistant_dashboard_messages"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."assistant_id" IS '@access: confidential';
-- Set comment to column: "oauth_server_issuer" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."oauth_server_issuer" IS '@access: restricted';
-- Set comment to column: "redirect_uri" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."redirect_uri" IS '@access: confidential';
-- Set comment to column: "client_id" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."client_id" IS '@access: restricted';
-- Set comment to column: "client_secret_encrypted" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."client_secret_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "client_secret_expires_at" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."client_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "registration_owner" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."registration_owner" IS '@access: confidential';
-- Set comment to column: "registration_started_at" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."registration_started_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."deleted" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_uri" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."client_id_metadata_uri" IS '@access: confidential';
-- Set comment to column: "id" on table: "assistant_mcp_servers"
COMMENT ON COLUMN "assistant_mcp_servers"."id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_mcp_servers"
COMMENT ON COLUMN "assistant_mcp_servers"."assistant_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "assistant_mcp_servers"
COMMENT ON COLUMN "assistant_mcp_servers"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "environment_id" on table: "assistant_mcp_servers"
COMMENT ON COLUMN "assistant_mcp_servers"."environment_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_mcp_servers"
COMMENT ON COLUMN "assistant_mcp_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_mcp_servers"
COMMENT ON COLUMN "assistant_mcp_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_mcp_servers"
COMMENT ON COLUMN "assistant_mcp_servers"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."assistant_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."organization_id" IS '@access: confidential';
-- Set comment to column: "content" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."content" IS '@access: opaque-restricted';
-- Set comment to column: "embedding" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."embedding" IS '@access: opaque-restricted';
-- Set comment to column: "supersedes_id" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."supersedes_id" IS '@access: confidential';
-- Set comment to column: "superseded_at" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."superseded_at" IS '@access: confidential';
-- Set comment to column: "valid_at" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."valid_at" IS '@access: confidential';
-- Set comment to column: "tags" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."tags" IS '@access: opaque-restricted';
-- Set comment to column: "origin_thread_id" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."origin_thread_id" IS '@access: confidential';
-- Set comment to column: "origin_chat_id" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."origin_chat_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."updated_at" IS '@access: confidential';
-- Set comment to column: "last_access" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."last_access" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "assistant_memories"
COMMENT ON COLUMN "assistant_memories"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."id" IS '@access: confidential';
-- Set comment to column: "assistant_thread_id" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."assistant_thread_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."assistant_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."project_id" IS '@access: confidential';
-- Set comment to column: "backend" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."backend" IS '@access: confidential';
-- Set comment to column: "state" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."state" IS '@access: confidential';
-- Set comment to column: "warm_until" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."warm_until" IS '@access: confidential';
-- Set comment to column: "lease_owner" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."lease_owner" IS '@access: confidential';
-- Set comment to column: "last_heartbeat_at" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."last_heartbeat_at" IS '@access: confidential';
-- Set comment to column: "backend_metadata_json" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."backend_metadata_json" IS '@access: restricted';
-- Set comment to column: "ended_at" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."ended_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."deleted" IS '@access: confidential';
-- Set comment to column: "ended" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."ended" IS '@access: confidential';
-- Set comment to column: "runtime_version" on table: "assistant_runtimes"
COMMENT ON COLUMN "assistant_runtimes"."runtime_version" IS '@access: confidential';
-- Set comment to column: "id" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."id" IS '@access: confidential';
-- Set comment to column: "assistant_thread_id" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."assistant_thread_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."assistant_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."project_id" IS '@access: confidential';
-- Set comment to column: "trigger_instance_id" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."trigger_instance_id" IS '@access: confidential';
-- Set comment to column: "event_id" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."event_id" IS '@access: confidential';
-- Set comment to column: "correlation_id" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."correlation_id" IS '@access: restricted';
-- Set comment to column: "status" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."status" IS '@access: confidential';
-- Set comment to column: "normalized_payload_json" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."normalized_payload_json" IS '@access: opaque-restricted';
-- Set comment to column: "source_payload_json" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."source_payload_json" IS '@access: opaque-restricted';
-- Set comment to column: "attempts" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."attempts" IS '@access: confidential';
-- Set comment to column: "last_error" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."last_error" IS '@access: opaque-restricted';
-- Set comment to column: "processed_at" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."processed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "assistant_thread_events"
COMMENT ON COLUMN "assistant_thread_events"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."assistant_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."project_id" IS '@access: confidential';
-- Set comment to column: "correlation_id" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."correlation_id" IS '@access: restricted';
-- Set comment to column: "chat_id" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."chat_id" IS '@access: confidential';
-- Set comment to column: "source_kind" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."source_kind" IS '@access: confidential';
-- Set comment to column: "source_ref_json" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."source_ref_json" IS '@access: opaque-restricted';
-- Set comment to column: "last_event_at" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."last_event_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."deleted" IS '@access: confidential';
-- Set comment to column: "skill_set_snapshot" on table: "assistant_threads"
COMMENT ON COLUMN "assistant_threads"."skill_set_snapshot" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "assistant_toolsets"
COMMENT ON COLUMN "assistant_toolsets"."id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "assistant_toolsets"
COMMENT ON COLUMN "assistant_toolsets"."assistant_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "assistant_toolsets"
COMMENT ON COLUMN "assistant_toolsets"."toolset_id" IS '@access: confidential';
-- Set comment to column: "environment_id" on table: "assistant_toolsets"
COMMENT ON COLUMN "assistant_toolsets"."environment_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistant_toolsets"
COMMENT ON COLUMN "assistant_toolsets"."project_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistant_toolsets"
COMMENT ON COLUMN "assistant_toolsets"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistant_toolsets"
COMMENT ON COLUMN "assistant_toolsets"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "assistants"
COMMENT ON COLUMN "assistants"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "assistants"
COMMENT ON COLUMN "assistants"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "assistants"
COMMENT ON COLUMN "assistants"."organization_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "assistants"
COMMENT ON COLUMN "assistants"."name" IS '@access: confidential';
-- Set comment to column: "model" on table: "assistants"
COMMENT ON COLUMN "assistants"."model" IS '@access: confidential';
-- Set comment to column: "instructions" on table: "assistants"
COMMENT ON COLUMN "assistants"."instructions" IS '@access: opaque-restricted';
-- Set comment to column: "warm_ttl_seconds" on table: "assistants"
COMMENT ON COLUMN "assistants"."warm_ttl_seconds" IS '@access: confidential';
-- Set comment to column: "max_concurrency" on table: "assistants"
COMMENT ON COLUMN "assistants"."max_concurrency" IS '@access: confidential';
-- Set comment to column: "status" on table: "assistants"
COMMENT ON COLUMN "assistants"."status" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "assistants"
COMMENT ON COLUMN "assistants"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "assistants"
COMMENT ON COLUMN "assistants"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "assistants"
COMMENT ON COLUMN "assistants"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "assistants"
COMMENT ON COLUMN "assistants"."deleted" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "assistants"
COMMENT ON COLUMN "assistants"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."id" IS '@access: confidential';
-- Set comment to column: "seq" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."seq" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."project_id" IS '@access: confidential';
-- Set comment to column: "actor_id" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."actor_id" IS '@access: restricted';
-- Set comment to column: "actor_type" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."actor_type" IS '@access: confidential';
-- Set comment to column: "actor_display_name" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."actor_display_name" IS '@access: confidential-pii';
-- Set comment to column: "actor_slug" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."actor_slug" IS '@access: confidential-pii';
-- Set comment to column: "action" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."action" IS '@access: confidential';
-- Set comment to column: "subject_id" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."subject_id" IS '@access: confidential';
-- Set comment to column: "subject_type" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."subject_type" IS '@access: confidential';
-- Set comment to column: "subject_display_name" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."subject_display_name" IS '@access: opaque-restricted';
-- Set comment to column: "subject_slug" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."subject_slug" IS '@access: confidential-pii';
-- Set comment to column: "before_snapshot" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."before_snapshot" IS '@access: opaque-restricted';
-- Set comment to column: "after_snapshot" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."after_snapshot" IS '@access: opaque-restricted';
-- Set comment to column: "metadata" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."metadata" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."created_at" IS '@access: confidential';
-- Set comment to column: "acting_surface" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."acting_surface" IS '@access: confidential';
-- Set comment to column: "acting_client_id" on table: "audit_logs"
COMMENT ON COLUMN "audit_logs"."acting_client_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."organization_id" IS '@access: confidential';
-- Set comment to column: "challenge_id" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."challenge_id" IS 'UUID of the denied challenge in the ClickHouse authz_challenges table.
@access: opaque-restricted';
-- Set comment to column: "principal_urn" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."principal_urn" IS 'The principal that was denied, copied from the challenge for query convenience.
@access: opaque-restricted';
-- Set comment to column: "scope" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."scope" IS '@access: opaque-restricted';
-- Set comment to column: "resource_kind" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."resource_kind" IS '@access: opaque-restricted';
-- Set comment to column: "resource_id" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."resource_id" IS '@access: opaque-restricted';
-- Set comment to column: "resolution_type" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."resolution_type" IS 'How the challenge was resolved: role_assigned, dismissed.
@access: confidential';
-- Set comment to column: "role_slug" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."role_slug" IS 'When resolution_type=role_assigned, the role slug that was assigned to the principal.
@access: confidential';
-- Set comment to column: "resolved_by" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."resolved_by" IS 'URN of the admin who resolved the challenge.
@access: confidential';
-- Set comment to column: "created_at" on table: "authz_challenge_resolutions"
COMMENT ON COLUMN "authz_challenge_resolutions"."created_at" IS '@access: confidential';
-- Set comment to column: "external_credential_id" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."external_credential_id" IS '@access: confidential';
-- Set comment to column: "external_credentials_provider" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."external_credentials_provider" IS '@access: confidential';
-- Set comment to column: "assume_role_arn" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."assume_role_arn" IS '@access: restricted';
-- Set comment to column: "external_id" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."external_id" IS '@access: secret-restricted';
-- Set comment to column: "oidc_audience" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."oidc_audience" IS '@access: restricted';
-- Set comment to column: "oidc_subject" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."oidc_subject" IS '@access: restricted';
-- Set comment to column: "sts_region" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."sts_region" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "aws_iam_credentials"
COMMENT ON COLUMN "aws_iam_credentials"."updated_at" IS '@access: confidential';
-- Set comment to column: "external_key_id" on table: "aws_kms_keys"
COMMENT ON COLUMN "aws_kms_keys"."external_key_id" IS '@access: confidential';
-- Set comment to column: "external_keys_provider" on table: "aws_kms_keys"
COMMENT ON COLUMN "aws_kms_keys"."external_keys_provider" IS '@access: confidential';
-- Set comment to column: "key_arn" on table: "aws_kms_keys"
COMMENT ON COLUMN "aws_kms_keys"."key_arn" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "aws_kms_keys"
COMMENT ON COLUMN "aws_kms_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "aws_kms_keys"
COMMENT ON COLUMN "aws_kms_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."organization_id" IS '@access: confidential';
-- Set comment to column: "cycle_start" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."cycle_start" IS '@access: confidential';
-- Set comment to column: "cycle_end" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."cycle_end" IS '@access: confidential';
-- Set comment to column: "tum_tokens" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."tum_tokens" IS '@access: confidential';
-- Set comment to column: "finalized_at" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."finalized_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."updated_at" IS '@access: confidential';
-- Set comment to column: "billed_tum_tokens" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."billed_tum_tokens" IS '@access: confidential';
-- Set comment to column: "billed_frozen_at" on table: "billing_cycle_usage"
COMMENT ON COLUMN "billing_cycle_usage"."billed_frozen_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."organization_id" IS '@access: confidential';
-- Set comment to column: "tum_monthly_token_limit" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."tum_monthly_token_limit" IS '@access: restricted';
-- Set comment to column: "alert_email" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."alert_email" IS '@access: confidential-pii';
-- Set comment to column: "billing_cycle_anchor_day" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."billing_cycle_anchor_day" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."updated_at" IS '@access: confidential';
-- Set comment to column: "tunneled_mcp_server_limit" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."tunneled_mcp_server_limit" IS 'Contracted org-level cap for tunneled MCP server sources. NULL means use the finite plan default.
@access: restricted';
-- Set comment to column: "stripe_customer_id" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_customer_id" IS '@access: restricted';
-- Set comment to column: "stripe_subscription_id" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_subscription_id" IS '@access: restricted';
-- Set comment to column: "stripe_billing_cycle_anchor" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_billing_cycle_anchor" IS '@access: confidential';
-- Set comment to column: "stripe_checkout_idempotency_key" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_checkout_idempotency_key" IS '@access: confidential';
-- Set comment to column: "stripe_checkout_billing_cycle_anchor" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_checkout_billing_cycle_anchor" IS '@access: confidential';
-- Set comment to column: "stripe_checkout_trial_end" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_checkout_trial_end" IS '@access: confidential';
-- Set comment to column: "stripe_checkout_expires_at" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_checkout_expires_at" IS '@access: confidential';
-- Set comment to column: "stripe_checkout_session_id" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_checkout_session_id" IS '@access: restricted';
-- Set comment to column: "id" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."organization_id" IS '@access: confidential';
-- Set comment to column: "body" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."body" IS '@access: opaque-restricted';
-- Set comment to column: "memory_type" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."memory_type" IS '@access: confidential';
-- Set comment to column: "structural_scope" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."structural_scope" IS '@access: confidential';
-- Set comment to column: "content_scope" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."content_scope" IS '@access: opaque-restricted';
-- Set comment to column: "embedding" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."embedding" IS '@access: opaque-restricted';
-- Set comment to column: "embedding_model" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."embedding_model" IS '@access: confidential';
-- Set comment to column: "extraction_model" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."extraction_model" IS '@access: confidential';
-- Set comment to column: "source_evaluation_id" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."source_evaluation_id" IS '@access: confidential';
-- Set comment to column: "source_candidate_index" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."source_candidate_index" IS '@access: confidential';
-- Set comment to column: "source_chat_id" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."source_chat_id" IS '@access: confidential';
-- Set comment to column: "source_turn" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."source_turn" IS '@access: confidential';
-- Set comment to column: "source_author_id" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."source_author_id" IS '@access: confidential-pii';
-- Set comment to column: "extracted_at" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."extracted_at" IS '@access: confidential';
-- Set comment to column: "lifecycle_state" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."lifecycle_state" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "business_memories"
COMMENT ON COLUMN "business_memories"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."project_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."chat_id" IS '@access: confidential';
-- Set comment to column: "session_id" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."session_id" IS '@access: confidential';
-- Set comment to column: "judge" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."judge" IS '@access: confidential';
-- Set comment to column: "observed_at" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."observed_at" IS '@access: confidential';
-- Set comment to column: "state" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."state" IS '@access: confidential';
-- Set comment to column: "reserved_on" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."reserved_on" IS '@access: confidential';
-- Set comment to column: "attempts" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."attempts" IS '@access: confidential';
-- Set comment to column: "last_error" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."last_error" IS '@access: opaque-restricted';
-- Set comment to column: "scored_at" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."scored_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "chat_analysis_evaluations"
COMMENT ON COLUMN "chat_analysis_evaluations"."updated_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "chat_analysis_settings"
COMMENT ON COLUMN "chat_analysis_settings"."organization_id" IS '@access: confidential';
-- Set comment to column: "judge" on table: "chat_analysis_settings"
COMMENT ON COLUMN "chat_analysis_settings"."judge" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "chat_analysis_settings"
COMMENT ON COLUMN "chat_analysis_settings"."enabled" IS '@access: confidential';
-- Set comment to column: "daily_cap" on table: "chat_analysis_settings"
COMMENT ON COLUMN "chat_analysis_settings"."daily_cap" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "chat_analysis_settings"
COMMENT ON COLUMN "chat_analysis_settings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "chat_analysis_settings"
COMMENT ON COLUMN "chat_analysis_settings"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."chat_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."project_id" IS '@access: confidential';
-- Set comment to column: "kind" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."kind" IS '@access: confidential';
-- Set comment to column: "content_asset_url" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."content_asset_url" IS '@access: confidential';
-- Set comment to column: "external_id" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."external_id" IS '@access: confidential';
-- Set comment to column: "parent_chat_message_id" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."parent_chat_message_id" IS '@access: confidential';
-- Set comment to column: "version" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."version" IS '@access: confidential';
-- Set comment to column: "source" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."source" IS '@access: confidential';
-- Set comment to column: "metadata" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."metadata" IS '@access: opaque-restricted';
-- Set comment to column: "risk_analyzed_at" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."risk_analyzed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "chat_content_parts"
COMMENT ON COLUMN "chat_content_parts"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."chat_id" IS '@access: confidential';
-- Set comment to column: "role" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."role" IS '@access: confidential';
-- Set comment to column: "content" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content" IS '@access: opaque-restricted';
-- Set comment to column: "model" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."model" IS '@access: confidential';
-- Set comment to column: "message_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."message_id" IS '@access: confidential';
-- Set comment to column: "tool_call_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_call_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."user_id" IS '@access: confidential';
-- Set comment to column: "finish_reason" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."finish_reason" IS '@access: confidential';
-- Set comment to column: "tool_calls" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_calls" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."created_at" IS '@access: confidential';
-- Set comment to column: "prompt_tokens" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."prompt_tokens" IS '@access: confidential';
-- Set comment to column: "completion_tokens" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."completion_tokens" IS '@access: confidential';
-- Set comment to column: "total_tokens" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."total_tokens" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."project_id" IS '@access: confidential';
-- Set comment to column: "external_user_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."external_user_id" IS '@access: confidential-pii';
-- Set comment to column: "tool_urn" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_urn" IS '@access: confidential';
-- Set comment to column: "tool_outcome" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_outcome" IS '@access: confidential';
-- Set comment to column: "tool_outcome_notes" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_outcome_notes" IS '@access: opaque-restricted';
-- Set comment to column: "origin" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."origin" IS '@access: opaque-restricted';
-- Set comment to column: "user_agent" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."user_agent" IS '@access: confidential-pii';
-- Set comment to column: "ip_address" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."ip_address" IS '@access: confidential-pii';
-- Set comment to column: "source" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."source" IS '@access: confidential';
-- Set comment to column: "content_raw" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_raw" IS '@access: opaque-restricted';
-- Set comment to column: "content_asset_url" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_asset_url" IS '@access: confidential';
-- Set comment to column: "storage_error" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."storage_error" IS '@access: opaque-restricted';
-- Set comment to column: "seq" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."seq" IS '@access: confidential';
-- Set comment to column: "content_hash" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_hash" IS '@access: confidential';
-- Set comment to column: "generation" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."generation" IS '@access: confidential';
-- Set comment to column: "risk_analyzed_at" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."risk_analyzed_at" IS '@access: confidential';
-- Set comment to column: "external_message_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."external_message_id" IS '@access: confidential';
-- Set comment to column: "replayed" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."replayed" IS '@access: confidential';
-- Set comment to column: "tool_call_summaries" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_call_summaries" IS '@access: opaque-restricted';
-- Set comment to column: "chat_resolution_id" on table: "chat_resolution_messages"
COMMENT ON COLUMN "chat_resolution_messages"."chat_resolution_id" IS '@access: confidential';
-- Set comment to column: "message_id" on table: "chat_resolution_messages"
COMMENT ON COLUMN "chat_resolution_messages"."message_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "chat_resolutions"
COMMENT ON COLUMN "chat_resolutions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chat_resolutions"
COMMENT ON COLUMN "chat_resolutions"."project_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "chat_resolutions"
COMMENT ON COLUMN "chat_resolutions"."chat_id" IS '@access: confidential';
-- Set comment to column: "user_goal" on table: "chat_resolutions"
COMMENT ON COLUMN "chat_resolutions"."user_goal" IS '@access: opaque-restricted';
-- Set comment to column: "resolution" on table: "chat_resolutions"
COMMENT ON COLUMN "chat_resolutions"."resolution" IS '@access: confidential';
-- Set comment to column: "resolution_notes" on table: "chat_resolutions"
COMMENT ON COLUMN "chat_resolutions"."resolution_notes" IS '@access: opaque-restricted';
-- Set comment to column: "score" on table: "chat_resolutions"
COMMENT ON COLUMN "chat_resolutions"."score" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "chat_resolutions"
COMMENT ON COLUMN "chat_resolutions"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."organization_id" IS '@access: confidential';
-- Set comment to column: "parent_chat_id" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."parent_chat_id" IS '@access: confidential';
-- Set comment to column: "child_chat_id" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."child_chat_id" IS '@access: confidential';
-- Set comment to column: "parent_session_id" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."parent_session_id" IS '@access: confidential';
-- Set comment to column: "child_session_id" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."child_session_id" IS '@access: confidential';
-- Set comment to column: "kind" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."kind" IS '@access: confidential';
-- Set comment to column: "target_harness" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."target_harness" IS '@access: confidential';
-- Set comment to column: "source_surface" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."source_surface" IS '@access: confidential';
-- Set comment to column: "actor_email" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."actor_email" IS '@access: confidential-pii';
-- Set comment to column: "device_serial" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."device_serial" IS '@access: confidential-pii';
-- Set comment to column: "device_hostname" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."device_hostname" IS '@access: confidential-pii';
-- Set comment to column: "created_at" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "chat_session_links"
COMMENT ON COLUMN "chat_session_links"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "chat_user_feedback"
COMMENT ON COLUMN "chat_user_feedback"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chat_user_feedback"
COMMENT ON COLUMN "chat_user_feedback"."project_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "chat_user_feedback"
COMMENT ON COLUMN "chat_user_feedback"."chat_id" IS '@access: confidential';
-- Set comment to column: "message_id" on table: "chat_user_feedback"
COMMENT ON COLUMN "chat_user_feedback"."message_id" IS '@access: confidential';
-- Set comment to column: "user_resolution" on table: "chat_user_feedback"
COMMENT ON COLUMN "chat_user_feedback"."user_resolution" IS '@access: confidential';
-- Set comment to column: "user_resolution_notes" on table: "chat_user_feedback"
COMMENT ON COLUMN "chat_user_feedback"."user_resolution_notes" IS '@access: opaque-restricted';
-- Set comment to column: "chat_resolution_id" on table: "chat_user_feedback"
COMMENT ON COLUMN "chat_user_feedback"."chat_resolution_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "chat_user_feedback"
COMMENT ON COLUMN "chat_user_feedback"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "chats"
COMMENT ON COLUMN "chats"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chats"
COMMENT ON COLUMN "chats"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "chats"
COMMENT ON COLUMN "chats"."organization_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "chats"
COMMENT ON COLUMN "chats"."user_id" IS '@access: confidential';
-- Set comment to column: "title" on table: "chats"
COMMENT ON COLUMN "chats"."title" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "chats"
COMMENT ON COLUMN "chats"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "chats"
COMMENT ON COLUMN "chats"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "chats"
COMMENT ON COLUMN "chats"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "chats"
COMMENT ON COLUMN "chats"."deleted" IS '@access: confidential';
-- Set comment to column: "external_user_id" on table: "chats"
COMMENT ON COLUMN "chats"."external_user_id" IS '@access: confidential-pii';
-- Set comment to column: "external_chat_id" on table: "chats"
COMMENT ON COLUMN "chats"."external_chat_id" IS '@access: confidential';
-- Set comment to column: "title_manually_set" on table: "chats"
COMMENT ON COLUMN "chats"."title_manually_set" IS '@access: confidential';
-- Set comment to column: "pinned_at" on table: "chats"
COMMENT ON COLUMN "chats"."pinned_at" IS '@access: confidential';
-- Set comment to column: "user_account_id" on table: "chats"
COMMENT ON COLUMN "chats"."user_account_id" IS '@access: confidential';
-- Set comment to column: "summary" on table: "chats"
COMMENT ON COLUMN "chats"."summary" IS '@access: opaque-restricted';
-- Set comment to column: "summary_generated_at" on table: "chats"
COMMENT ON COLUMN "chats"."summary_generated_at" IS '@access: confidential';
-- Set comment to column: "litellm_proxied" on table: "chats"
COMMENT ON COLUMN "chats"."litellm_proxied" IS '@access: confidential';
-- Set comment to column: "cwd" on table: "chats"
COMMENT ON COLUMN "chats"."cwd" IS '@access: opaque-restricted';
-- Set comment to column: "inference_accepted_checkpoint" on table: "chats"
COMMENT ON COLUMN "chats"."inference_accepted_checkpoint" IS '@access: confidential';
-- Set comment to column: "id" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."id" IS '@access: confidential';
-- Set comment to column: "domain" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."domain" IS '@access: restricted';
-- Set comment to column: "verified" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."verified" IS '@access: confidential';
-- Set comment to column: "ingress_name" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."ingress_name" IS '@access: restricted';
-- Set comment to column: "cert_secret_name" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."cert_secret_name" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."deleted" IS '@access: confidential';
-- Set comment to column: "activated" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."activated" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."organization_id" IS '@access: confidential';
-- Set comment to column: "provisioner_kind" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."provisioner_kind" IS '@access: confidential';
-- Set comment to column: "ip_allowlist" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."ip_allowlist" IS '@access: opaque-restricted';
-- Set comment to column: "health_status" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."health_status" IS '@access: confidential';
-- Set comment to column: "health_issue" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."health_issue" IS '@access: confidential';
-- Set comment to column: "health_checked_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."health_checked_at" IS '@access: confidential';
-- Set comment to column: "unhealthy_since" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."unhealthy_since" IS '@access: confidential';
-- Set comment to column: "certificate_expires_at" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."certificate_expires_at" IS '@access: confidential';
-- Set comment to column: "consecutive_failures" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."consecutive_failures" IS '@access: confidential';
-- Set comment to column: "openai_apps_challenge_token" on table: "custom_domains"
COMMENT ON COLUMN "custom_domains"."openai_apps_challenge_token" IS '@access: secret-restricted';
-- Set comment to column: "id" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."project_id" IS '@access: confidential';
-- Set comment to column: "data_source" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."data_source" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."enabled" IS '@access: confidential';
-- Set comment to column: "otel_destination_id" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."otel_destination_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "data_export_routes"
COMMENT ON COLUMN "data_export_routes"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."id" IS '@access: confidential';
-- Set comment to column: "seq" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."seq" IS '@access: confidential';
-- Set comment to column: "event" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."event" IS '@access: confidential';
-- Set comment to column: "message" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."message" IS '@access: opaque-restricted';
-- Set comment to column: "deployment_id" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."deployment_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."project_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."updated_at" IS '@access: confidential';
-- Set comment to column: "attachment_id" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."attachment_id" IS '@access: confidential';
-- Set comment to column: "attachment_type" on table: "deployment_logs"
COMMENT ON COLUMN "deployment_logs"."attachment_type" IS '@access: confidential';
-- Set comment to column: "id" on table: "deployment_statuses"
COMMENT ON COLUMN "deployment_statuses"."id" IS '@access: confidential';
-- Set comment to column: "seq" on table: "deployment_statuses"
COMMENT ON COLUMN "deployment_statuses"."seq" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "deployment_statuses"
COMMENT ON COLUMN "deployment_statuses"."deployment_id" IS '@access: confidential';
-- Set comment to column: "status" on table: "deployment_statuses"
COMMENT ON COLUMN "deployment_statuses"."status" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "deployment_statuses"
COMMENT ON COLUMN "deployment_statuses"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "deployment_statuses"
COMMENT ON COLUMN "deployment_statuses"."updated_at" IS '@access: confidential';
-- Set comment to column: "changed_at" on table: "deployment_tag_history"
COMMENT ON COLUMN "deployment_tag_history"."changed_at" IS '@access: confidential';
-- Set comment to column: "changed_by" on table: "deployment_tag_history"
COMMENT ON COLUMN "deployment_tag_history"."changed_by" IS '@access: confidential';
-- Set comment to column: "id" on table: "deployment_tag_history"
COMMENT ON COLUMN "deployment_tag_history"."id" IS '@access: confidential';
-- Set comment to column: "tag_id" on table: "deployment_tag_history"
COMMENT ON COLUMN "deployment_tag_history"."tag_id" IS '@access: confidential';
-- Set comment to column: "previous_deployment_id" on table: "deployment_tag_history"
COMMENT ON COLUMN "deployment_tag_history"."previous_deployment_id" IS '@access: confidential';
-- Set comment to column: "new_deployment_id" on table: "deployment_tag_history"
COMMENT ON COLUMN "deployment_tag_history"."new_deployment_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "deployment_tags"
COMMENT ON COLUMN "deployment_tags"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "deployment_tags"
COMMENT ON COLUMN "deployment_tags"."updated_at" IS '@access: confidential';
-- Set comment to column: "name" on table: "deployment_tags"
COMMENT ON COLUMN "deployment_tags"."name" IS '@access: confidential';
-- Set comment to column: "id" on table: "deployment_tags"
COMMENT ON COLUMN "deployment_tags"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "deployment_tags"
COMMENT ON COLUMN "deployment_tags"."project_id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "deployment_tags"
COMMENT ON COLUMN "deployment_tags"."deployment_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "deployments"
COMMENT ON COLUMN "deployments"."id" IS '@access: confidential';
-- Set comment to column: "seq" on table: "deployments"
COMMENT ON COLUMN "deployments"."seq" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "deployments"
COMMENT ON COLUMN "deployments"."user_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "deployments"
COMMENT ON COLUMN "deployments"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "deployments"
COMMENT ON COLUMN "deployments"."organization_id" IS '@access: confidential';
-- Set comment to column: "idempotency_key" on table: "deployments"
COMMENT ON COLUMN "deployments"."idempotency_key" IS '@access: opaque-restricted';
-- Set comment to column: "cloned_from" on table: "deployments"
COMMENT ON COLUMN "deployments"."cloned_from" IS '@access: confidential';
-- Set comment to column: "github_repo" on table: "deployments"
COMMENT ON COLUMN "deployments"."github_repo" IS '@access: confidential-pii';
-- Set comment to column: "github_pr" on table: "deployments"
COMMENT ON COLUMN "deployments"."github_pr" IS '@access: confidential';
-- Set comment to column: "github_sha" on table: "deployments"
COMMENT ON COLUMN "deployments"."github_sha" IS '@access: confidential';
-- Set comment to column: "external_id" on table: "deployments"
COMMENT ON COLUMN "deployments"."external_id" IS '@access: opaque-restricted';
-- Set comment to column: "external_url" on table: "deployments"
COMMENT ON COLUMN "deployments"."external_url" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "deployments"
COMMENT ON COLUMN "deployments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "deployments"
COMMENT ON COLUMN "deployments"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."deployment_id" IS '@access: confidential';
-- Set comment to column: "asset_id" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."asset_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."slug" IS '@access: confidential';
-- Set comment to column: "runtime" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."runtime" IS '@access: confidential';
-- Set comment to column: "runner_version" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."runner_version" IS '@access: confidential';
-- Set comment to column: "memory_mib" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."memory_mib" IS '@access: confidential';
-- Set comment to column: "scale" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."scale" IS '@access: confidential';
-- Set comment to column: "memory_mib_override" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."memory_mib_override" IS '@access: confidential';
-- Set comment to column: "scale_override" on table: "deployments_functions"
COMMENT ON COLUMN "deployments_functions"."scale_override" IS '@access: confidential';
-- Set comment to column: "id" on table: "deployments_openapiv3_assets"
COMMENT ON COLUMN "deployments_openapiv3_assets"."id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "deployments_openapiv3_assets"
COMMENT ON COLUMN "deployments_openapiv3_assets"."deployment_id" IS '@access: confidential';
-- Set comment to column: "asset_id" on table: "deployments_openapiv3_assets"
COMMENT ON COLUMN "deployments_openapiv3_assets"."asset_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "deployments_openapiv3_assets"
COMMENT ON COLUMN "deployments_openapiv3_assets"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "deployments_openapiv3_assets"
COMMENT ON COLUMN "deployments_openapiv3_assets"."slug" IS '@access: confidential';
-- Set comment to column: "id" on table: "deployments_packages"
COMMENT ON COLUMN "deployments_packages"."id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "deployments_packages"
COMMENT ON COLUMN "deployments_packages"."deployment_id" IS '@access: confidential';
-- Set comment to column: "package_id" on table: "deployments_packages"
COMMENT ON COLUMN "deployments_packages"."package_id" IS '@access: confidential';
-- Set comment to column: "version_id" on table: "deployments_packages"
COMMENT ON COLUMN "deployments_packages"."version_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "device_agent_configurations"
COMMENT ON COLUMN "device_agent_configurations"."organization_id" IS '@access: confidential';
-- Set comment to column: "schema_version" on table: "device_agent_configurations"
COMMENT ON COLUMN "device_agent_configurations"."schema_version" IS '@access: confidential';
-- Set comment to column: "config" on table: "device_agent_configurations"
COMMENT ON COLUMN "device_agent_configurations"."config" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "device_agent_configurations"
COMMENT ON COLUMN "device_agent_configurations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_agent_configurations"
COMMENT ON COLUMN "device_agent_configurations"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."organization_id" IS '@access: confidential';
-- Set comment to column: "serial_number" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."serial_number" IS '@access: confidential-pii';
-- Set comment to column: "email" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."email" IS '@access: confidential-pii';
-- Set comment to column: "hostname" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."hostname" IS '@access: confidential-pii';
-- Set comment to column: "first_seen_at" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_agent_device_syncs"
COMMENT ON COLUMN "device_agent_device_syncs"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."organization_id" IS '@access: confidential';
-- Set comment to column: "email" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."email" IS '@access: confidential-pii';
-- Set comment to column: "environment" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."environment" IS '@access: confidential';
-- Set comment to column: "hostname" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."hostname" IS '@access: confidential-pii';
-- Set comment to column: "first_seen_at" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_agent_environment_syncs"
COMMENT ON COLUMN "device_agent_environment_syncs"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."organization_id" IS '@access: confidential';
-- Set comment to column: "email" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."email" IS '@access: confidential-pii';
-- Set comment to column: "first_seen_at" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_agent_syncs"
COMMENT ON COLUMN "device_agent_syncs"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."organization_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."provider" IS '@access: confidential';
-- Set comment to column: "credentials_encrypted" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."credentials_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "settings" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."settings" IS '@access: opaque-restricted';
-- Set comment to column: "enabled" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."enabled" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "device_integration_schedules"
COMMENT ON COLUMN "device_integration_schedules"."id" IS '@access: confidential';
-- Set comment to column: "device_integration_config_id" on table: "device_integration_schedules"
COMMENT ON COLUMN "device_integration_schedules"."device_integration_config_id" IS '@access: confidential';
-- Set comment to column: "schedule" on table: "device_integration_schedules"
COMMENT ON COLUMN "device_integration_schedules"."schedule" IS '@access: confidential';
-- Set comment to column: "disabled_at" on table: "device_integration_schedules"
COMMENT ON COLUMN "device_integration_schedules"."disabled_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "device_integration_schedules"
COMMENT ON COLUMN "device_integration_schedules"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_integration_schedules"
COMMENT ON COLUMN "device_integration_schedules"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."id" IS '@access: confidential';
-- Set comment to column: "device_integration_schedule_id" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."device_integration_schedule_id" IS '@access: confidential';
-- Set comment to column: "poll_watermark_at" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."poll_watermark_at" IS '@access: confidential';
-- Set comment to column: "next_poll_after" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."next_poll_after" IS '@access: confidential';
-- Set comment to column: "last_poll_success_at" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."last_poll_success_at" IS '@access: confidential';
-- Set comment to column: "last_poll_failed_at" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."last_poll_failed_at" IS '@access: confidential';
-- Set comment to column: "last_poll_error" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."last_poll_error" IS '@access: opaque-restricted';
-- Set comment to column: "consecutive_failures" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."consecutive_failures" IS '@access: confidential';
-- Set comment to column: "consecutive_auth_rejections" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."consecutive_auth_rejections" IS '@access: confidential';
-- Set comment to column: "last_push_digest" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."last_push_digest" IS '@access: confidential';
-- Set comment to column: "auto_paused_at" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."auto_paused_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_integration_syncs"
COMMENT ON COLUMN "device_integration_syncs"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."organization_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."provider" IS '@access: confidential';
-- Set comment to column: "device_id" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."device_id" IS '@access: confidential-pii';
-- Set comment to column: "linked_user_id" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."linked_user_id" IS '@access: confidential';
-- Set comment to column: "first_seen_at" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "device_owners"
COMMENT ON COLUMN "device_owners"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."organization_id" IS '@access: confidential';
-- Set comment to column: "workos_directory_group_id" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."workos_directory_group_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."name" IS '@access: confidential';
-- Set comment to column: "attributes" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."attributes" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."deleted" IS '@access: confidential';
-- Set comment to column: "workos_created_at" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."workos_created_at" IS '@access: confidential';
-- Set comment to column: "workos_updated_at" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."workos_updated_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted_at" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."workos_deleted_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."workos_deleted" IS '@access: confidential';
-- Set comment to column: "workos_last_event_id" on table: "directory_groups"
COMMENT ON COLUMN "directory_groups"."workos_last_event_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."organization_id" IS '@access: confidential';
-- Set comment to column: "source_kind" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."source_kind" IS '@access: confidential';
-- Set comment to column: "directory_group_id" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."directory_group_id" IS '@access: confidential';
-- Set comment to column: "attribute_key" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."attribute_key" IS '@access: opaque-restricted';
-- Set comment to column: "attribute_value" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."attribute_value" IS '@access: opaque-restricted';
-- Set comment to column: "role_urn" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."role_urn" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "directory_role_mappings"
COMMENT ON COLUMN "directory_role_mappings"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."id" IS '@access: confidential';
-- Set comment to column: "directory_user_id" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."directory_user_id" IS '@access: confidential';
-- Set comment to column: "directory_group_id" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."directory_group_id" IS '@access: confidential';
-- Set comment to column: "workos_directory_user_id" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."workos_directory_user_id" IS '@access: confidential-pii';
-- Set comment to column: "workos_directory_group_id" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."workos_directory_group_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."deleted" IS '@access: confidential';
-- Set comment to column: "workos_created_at" on table: "directory_user_group_memberships"
COMMENT ON COLUMN "directory_user_group_memberships"."workos_created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."organization_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."user_id" IS '@access: confidential';
-- Set comment to column: "workos_directory_user_id" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."workos_directory_user_id" IS '@access: confidential-pii';
-- Set comment to column: "email" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."email" IS '@access: confidential-pii';
-- Set comment to column: "attributes" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."attributes" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."deleted" IS '@access: confidential';
-- Set comment to column: "workos_created_at" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."workos_created_at" IS '@access: confidential';
-- Set comment to column: "workos_updated_at" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."workos_updated_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted_at" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."workos_deleted_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."workos_deleted" IS '@access: confidential';
-- Set comment to column: "workos_last_event_id" on table: "directory_users"
COMMENT ON COLUMN "directory_users"."workos_last_event_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "environment_entries"
COMMENT ON COLUMN "environment_entries"."name" IS '@access: confidential';
-- Set comment to column: "value" on table: "environment_entries"
COMMENT ON COLUMN "environment_entries"."value" IS '@access: secret-restricted';
-- Set comment to column: "environment_id" on table: "environment_entries"
COMMENT ON COLUMN "environment_entries"."environment_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "environment_entries"
COMMENT ON COLUMN "environment_entries"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "environment_entries"
COMMENT ON COLUMN "environment_entries"."updated_at" IS '@access: confidential';
-- Set comment to column: "is_secret" on table: "environment_entries"
COMMENT ON COLUMN "environment_entries"."is_secret" IS '@access: confidential';
-- Set comment to column: "id" on table: "environments"
COMMENT ON COLUMN "environments"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "environments"
COMMENT ON COLUMN "environments"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "environments"
COMMENT ON COLUMN "environments"."project_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "environments"
COMMENT ON COLUMN "environments"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "environments"
COMMENT ON COLUMN "environments"."slug" IS '@access: confidential';
-- Set comment to column: "description" on table: "environments"
COMMENT ON COLUMN "environments"."description" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "environments"
COMMENT ON COLUMN "environments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "environments"
COMMENT ON COLUMN "environments"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "environments"
COMMENT ON COLUMN "environments"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "environments"
COMMENT ON COLUMN "environments"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."project_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."provider" IS '@access: confidential';
-- Set comment to column: "name" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "external_credentials"
COMMENT ON COLUMN "external_credentials"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."project_id" IS '@access: confidential';
-- Set comment to column: "external_credential_id" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."external_credential_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."provider" IS '@access: confidential';
-- Set comment to column: "algorithm" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."algorithm" IS '@access: confidential';
-- Set comment to column: "name" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."name" IS '@access: confidential';
-- Set comment to column: "customer_grant_reference" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."customer_grant_reference" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."deleted" IS '@access: confidential';
-- Set comment to column: "identity_provider_connection_id" on table: "external_keys"
COMMENT ON COLUMN "external_keys"."identity_provider_connection_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."deployment_id" IS '@access: confidential';
-- Set comment to column: "registry_id" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."registry_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."slug" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."deleted" IS '@access: confidential';
-- Set comment to column: "registry_server_specifier" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."registry_server_specifier" IS '@access: confidential';
-- Set comment to column: "selected_remotes" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."selected_remotes" IS '@access: opaque-restricted';
-- Set comment to column: "organization_mcp_collection_registry_id" on table: "external_mcp_attachments"
COMMENT ON COLUMN "external_mcp_attachments"."organization_mcp_collection_registry_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."id" IS '@access: confidential';
-- Set comment to column: "external_mcp_attachment_id" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."external_mcp_attachment_id" IS '@access: confidential';
-- Set comment to column: "tool_urn" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."tool_urn" IS '@access: confidential';
-- Set comment to column: "remote_url" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."remote_url" IS '@access: restricted';
-- Set comment to column: "requires_oauth" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."requires_oauth" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."deleted" IS '@access: confidential';
-- Set comment to column: "oauth_version" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."oauth_version" IS '@access: confidential';
-- Set comment to column: "oauth_authorization_endpoint" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."oauth_authorization_endpoint" IS '@access: restricted';
-- Set comment to column: "oauth_token_endpoint" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."oauth_token_endpoint" IS '@access: restricted';
-- Set comment to column: "oauth_registration_endpoint" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."oauth_registration_endpoint" IS '@access: restricted';
-- Set comment to column: "oauth_scopes_supported" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."oauth_scopes_supported" IS '@access: opaque-restricted';
-- Set comment to column: "transport_type" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."transport_type" IS '@access: confidential';
-- Set comment to column: "type" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."type" IS '@access: confidential';
-- Set comment to column: "name" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."description" IS '@access: opaque-restricted';
-- Set comment to column: "schema" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."schema" IS '@access: opaque-restricted';
-- Set comment to column: "header_definitions" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."header_definitions" IS '@access: opaque-restricted';
-- Set comment to column: "title" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."title" IS '@access: confidential';
-- Set comment to column: "read_only_hint" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."read_only_hint" IS '@access: confidential';
-- Set comment to column: "destructive_hint" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."destructive_hint" IS '@access: confidential';
-- Set comment to column: "idempotent_hint" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."idempotent_hint" IS '@access: confidential';
-- Set comment to column: "open_world_hint" on table: "external_mcp_tool_definitions"
COMMENT ON COLUMN "external_mcp_tool_definitions"."open_world_hint" IS '@access: confidential';
-- Set comment to column: "id" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."project_id" IS '@access: confidential';
-- Set comment to column: "oauth_server_issuer" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."oauth_server_issuer" IS '@access: restricted';
-- Set comment to column: "client_id" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."client_id" IS '@access: restricted';
-- Set comment to column: "client_secret_encrypted" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."client_secret_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "client_id_issued_at" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."client_id_issued_at" IS '@access: confidential';
-- Set comment to column: "client_secret_expires_at" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."client_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "external_oauth_client_registrations"
COMMENT ON COLUMN "external_oauth_client_registrations"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."project_id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."slug" IS '@access: confidential';
-- Set comment to column: "metadata" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."metadata" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."deleted" IS '@access: confidential';
-- Set comment to column: "authorization_server_issuer" on table: "external_oauth_server_metadata"
COMMENT ON COLUMN "external_oauth_server_metadata"."authorization_server_issuer" IS '@access: restricted';
-- Set comment to column: "id" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."id" IS '@access: confidential';
-- Set comment to column: "seq" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."seq" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."project_id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."deployment_id" IS '@access: confidential';
-- Set comment to column: "function_id" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."function_id" IS '@access: confidential';
-- Set comment to column: "access_id" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."access_id" IS '@access: confidential';
-- Set comment to column: "fly_org_id" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."fly_org_id" IS '@access: confidential';
-- Set comment to column: "fly_org_slug" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."fly_org_slug" IS '@access: confidential';
-- Set comment to column: "app_name" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."app_name" IS '@access: confidential';
-- Set comment to column: "app_url" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."app_url" IS '@access: confidential';
-- Set comment to column: "runner_version" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."runner_version" IS '@access: confidential';
-- Set comment to column: "primary_region" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."primary_region" IS '@access: confidential';
-- Set comment to column: "status" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."status" IS '@access: confidential';
-- Set comment to column: "reaped_at" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."reaped_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."updated_at" IS '@access: confidential';
-- Set comment to column: "reap_error" on table: "fly_apps"
COMMENT ON COLUMN "fly_apps"."reap_error" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."id" IS '@access: confidential';
-- Set comment to column: "resource_urn" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."resource_urn" IS '@access: restricted';
-- Set comment to column: "project_id" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."project_id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."deployment_id" IS '@access: confidential';
-- Set comment to column: "function_id" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."function_id" IS '@access: confidential';
-- Set comment to column: "runtime" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."runtime" IS '@access: confidential';
-- Set comment to column: "name" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."description" IS '@access: opaque-restricted';
-- Set comment to column: "uri" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."uri" IS '@access: restricted';
-- Set comment to column: "title" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."title" IS '@access: confidential';
-- Set comment to column: "mime_type" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."mime_type" IS '@access: confidential';
-- Set comment to column: "variables" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."variables" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."deleted" IS '@access: confidential';
-- Set comment to column: "meta" on table: "function_resource_definitions"
COMMENT ON COLUMN "function_resource_definitions"."meta" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."id" IS '@access: confidential';
-- Set comment to column: "tool_urn" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."tool_urn" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."deployment_id" IS '@access: confidential';
-- Set comment to column: "function_id" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."function_id" IS '@access: confidential';
-- Set comment to column: "runtime" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."runtime" IS '@access: confidential';
-- Set comment to column: "name" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."description" IS '@access: opaque-restricted';
-- Set comment to column: "input_schema" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."input_schema" IS '@access: opaque-restricted';
-- Set comment to column: "variables" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."variables" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."deleted" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."project_id" IS '@access: confidential';
-- Set comment to column: "meta" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."meta" IS '@access: opaque-restricted';
-- Set comment to column: "auth_input" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."auth_input" IS '@access: opaque-restricted';
-- Set comment to column: "read_only_hint" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."read_only_hint" IS '@access: confidential';
-- Set comment to column: "destructive_hint" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."destructive_hint" IS '@access: confidential';
-- Set comment to column: "idempotent_hint" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."idempotent_hint" IS '@access: confidential';
-- Set comment to column: "open_world_hint" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."open_world_hint" IS '@access: confidential';
-- Set comment to column: "tags" on table: "function_tool_definitions"
COMMENT ON COLUMN "function_tool_definitions"."tags" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."id" IS '@access: confidential';
-- Set comment to column: "seq" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."seq" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."project_id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."deployment_id" IS '@access: confidential';
-- Set comment to column: "function_id" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."function_id" IS '@access: confidential';
-- Set comment to column: "encryption_key" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."encryption_key" IS '@access: secret-restricted';
-- Set comment to column: "bearer_format" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."bearer_format" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."deleted" IS '@access: confidential';
-- Set comment to column: "external_credential_id" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."external_credential_id" IS '@access: confidential';
-- Set comment to column: "external_credentials_provider" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."external_credentials_provider" IS '@access: confidential';
-- Set comment to column: "impersonate_service_account" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."impersonate_service_account" IS '@access: restricted';
-- Set comment to column: "wif_pool_id" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."wif_pool_id" IS '@access: restricted';
-- Set comment to column: "wif_provider_id" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."wif_provider_id" IS '@access: restricted';
-- Set comment to column: "wif_project_number" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."wif_project_number" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."updated_at" IS '@access: confidential';
-- Set comment to column: "skip_project_verification" on table: "gcp_iam_credentials"
COMMENT ON COLUMN "gcp_iam_credentials"."skip_project_verification" IS '@access: confidential';
-- Set comment to column: "external_key_id" on table: "gcp_kms_keys"
COMMENT ON COLUMN "gcp_kms_keys"."external_key_id" IS '@access: confidential';
-- Set comment to column: "external_keys_provider" on table: "gcp_kms_keys"
COMMENT ON COLUMN "gcp_kms_keys"."external_keys_provider" IS '@access: confidential';
-- Set comment to column: "resource_name" on table: "gcp_kms_keys"
COMMENT ON COLUMN "gcp_kms_keys"."resource_name" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "gcp_kms_keys"
COMMENT ON COLUMN "gcp_kms_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "gcp_kms_keys"
COMMENT ON COLUMN "gcp_kms_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."id" IS '@access: confidential';
-- Set comment to column: "workos_slug" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."workos_slug" IS '@access: confidential';
-- Set comment to column: "workos_name" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."workos_name" IS '@access: confidential';
-- Set comment to column: "workos_description" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."workos_description" IS '@access: opaque-restricted';
-- Set comment to column: "workos_created_at" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."workos_created_at" IS '@access: confidential';
-- Set comment to column: "workos_updated_at" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."workos_updated_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted_at" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."workos_deleted_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."workos_deleted" IS '@access: confidential';
-- Set comment to column: "workos_last_event_id" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."workos_last_event_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "global_roles"
COMMENT ON COLUMN "global_roles"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "hooks_server_name_overrides"
COMMENT ON COLUMN "hooks_server_name_overrides"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "hooks_server_name_overrides"
COMMENT ON COLUMN "hooks_server_name_overrides"."project_id" IS '@access: confidential';
-- Set comment to column: "raw_server_name" on table: "hooks_server_name_overrides"
COMMENT ON COLUMN "hooks_server_name_overrides"."raw_server_name" IS '@access: confidential';
-- Set comment to column: "display_name" on table: "hooks_server_name_overrides"
COMMENT ON COLUMN "hooks_server_name_overrides"."display_name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "hooks_server_name_overrides"
COMMENT ON COLUMN "hooks_server_name_overrides"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "hooks_server_name_overrides"
COMMENT ON COLUMN "hooks_server_name_overrides"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "http_security"
COMMENT ON COLUMN "http_security"."id" IS '@access: confidential';
-- Set comment to column: "key" on table: "http_security"
COMMENT ON COLUMN "http_security"."key" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "http_security"
COMMENT ON COLUMN "http_security"."deployment_id" IS '@access: confidential';
-- Set comment to column: "type" on table: "http_security"
COMMENT ON COLUMN "http_security"."type" IS '@access: confidential';
-- Set comment to column: "name" on table: "http_security"
COMMENT ON COLUMN "http_security"."name" IS '@access: confidential';
-- Set comment to column: "in_placement" on table: "http_security"
COMMENT ON COLUMN "http_security"."in_placement" IS '@access: confidential';
-- Set comment to column: "scheme" on table: "http_security"
COMMENT ON COLUMN "http_security"."scheme" IS '@access: confidential';
-- Set comment to column: "bearer_format" on table: "http_security"
COMMENT ON COLUMN "http_security"."bearer_format" IS '@access: confidential';
-- Set comment to column: "env_variables" on table: "http_security"
COMMENT ON COLUMN "http_security"."env_variables" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "http_security"
COMMENT ON COLUMN "http_security"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "http_security"
COMMENT ON COLUMN "http_security"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "http_security"
COMMENT ON COLUMN "http_security"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "http_security"
COMMENT ON COLUMN "http_security"."deleted" IS '@access: confidential';
-- Set comment to column: "oauth_types" on table: "http_security"
COMMENT ON COLUMN "http_security"."oauth_types" IS '@access: confidential';
-- Set comment to column: "oauth_flows" on table: "http_security"
COMMENT ON COLUMN "http_security"."oauth_flows" IS '@access: opaque-restricted';
-- Set comment to column: "project_id" on table: "http_security"
COMMENT ON COLUMN "http_security"."project_id" IS '@access: confidential';
-- Set comment to column: "openapiv3_document_id" on table: "http_security"
COMMENT ON COLUMN "http_security"."openapiv3_document_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."project_id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."deployment_id" IS '@access: confidential';
-- Set comment to column: "openapiv3_document_id" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."openapiv3_document_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."name" IS '@access: confidential';
-- Set comment to column: "summary" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."summary" IS '@access: opaque-restricted';
-- Set comment to column: "description" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."description" IS '@access: opaque-restricted';
-- Set comment to column: "openapiv3_operation" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."openapiv3_operation" IS '@access: confidential';
-- Set comment to column: "tags" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."tags" IS '@access: opaque-restricted';
-- Set comment to column: "server_env_var" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."server_env_var" IS '@access: confidential';
-- Set comment to column: "default_server_url" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."default_server_url" IS '@access: restricted';
-- Set comment to column: "security" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."security" IS '@access: opaque-restricted';
-- Set comment to column: "http_method" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."http_method" IS '@access: confidential';
-- Set comment to column: "path" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."path" IS '@access: restricted';
-- Set comment to column: "schema_version" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."schema_version" IS '@access: confidential';
-- Set comment to column: "schema" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."schema" IS '@access: opaque-restricted';
-- Set comment to column: "header_settings" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."header_settings" IS '@access: opaque-restricted';
-- Set comment to column: "query_settings" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."query_settings" IS '@access: opaque-restricted';
-- Set comment to column: "path_settings" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."path_settings" IS '@access: opaque-restricted';
-- Set comment to column: "request_content_type" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."request_content_type" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."deleted" IS '@access: confidential';
-- Set comment to column: "confirm" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."confirm" IS '@access: confidential';
-- Set comment to column: "confirm_prompt" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."confirm_prompt" IS '@access: opaque-restricted';
-- Set comment to column: "x_gram" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."x_gram" IS '@access: confidential';
-- Set comment to column: "original_name" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."original_name" IS '@access: confidential';
-- Set comment to column: "original_summary" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."original_summary" IS '@access: opaque-restricted';
-- Set comment to column: "original_description" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."original_description" IS '@access: opaque-restricted';
-- Set comment to column: "summarizer" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."summarizer" IS '@access: opaque-restricted';
-- Set comment to column: "response_filter" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."response_filter" IS '@access: opaque-restricted';
-- Set comment to column: "untruncated_name" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."untruncated_name" IS '@access: confidential';
-- Set comment to column: "tool_urn" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."tool_urn" IS '@access: confidential';
-- Set comment to column: "read_only_hint" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."read_only_hint" IS '@access: confidential';
-- Set comment to column: "destructive_hint" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."destructive_hint" IS '@access: confidential';
-- Set comment to column: "idempotent_hint" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."idempotent_hint" IS '@access: confidential';
-- Set comment to column: "open_world_hint" on table: "http_tool_definitions"
COMMENT ON COLUMN "http_tool_definitions"."open_world_hint" IS '@access: confidential';
-- Set comment to column: "id" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."organization_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."provider" IS '@access: confidential';
-- Set comment to column: "status" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."status" IS '@access: confidential';
-- Set comment to column: "last_verified_at" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."last_verified_at" IS '@access: confidential';
-- Set comment to column: "last_error" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."last_error" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "identity_provider_connections"
COMMENT ON COLUMN "identity_provider_connections"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."project_id" IS '@access: confidential';
-- Set comment to column: "external_key_id" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."external_key_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."deleted" IS '@access: confidential';
-- Set comment to column: "identity_provider_connection_id" on table: "json_web_key_sets"
COMMENT ON COLUMN "json_web_key_sets"."identity_provider_connection_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."project_id" IS '@access: confidential';
-- Set comment to column: "json_web_key_set_id" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."json_web_key_set_id" IS '@access: confidential';
-- Set comment to column: "external_key_id" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."external_key_id" IS '@access: confidential';
-- Set comment to column: "external_key_version" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."external_key_version" IS '@access: restricted';
-- Set comment to column: "state" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."state" IS '@access: confidential';
-- Set comment to column: "kid" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."kid" IS '@access: confidential';
-- Set comment to column: "public_jwk" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."public_jwk" IS '@access: confidential';
-- Set comment to column: "activated_at" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."activated_at" IS '@access: confidential';
-- Set comment to column: "retired_at" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."retired_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "json_web_keys"
COMMENT ON COLUMN "json_web_keys"."deleted" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "killswitch_expiry_events"
COMMENT ON COLUMN "killswitch_expiry_events"."organization_id" IS '@access: confidential';
-- Set comment to column: "prescription_id" on table: "killswitch_expiry_events"
COMMENT ON COLUMN "killswitch_expiry_events"."prescription_id" IS '@access: confidential';
-- Set comment to column: "version" on table: "killswitch_expiry_events"
COMMENT ON COLUMN "killswitch_expiry_events"."version" IS '@access: confidential';
-- Set comment to column: "recorded_at" on table: "killswitch_expiry_events"
COMMENT ON COLUMN "killswitch_expiry_events"."recorded_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."organization_id" IS '@access: confidential';
-- Set comment to column: "operation_id" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."operation_id" IS '@access: confidential';
-- Set comment to column: "actor_user_id" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."actor_user_id" IS '@access: confidential';
-- Set comment to column: "operation" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."operation" IS '@access: confidential';
-- Set comment to column: "request_hash" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."request_hash" IS '@access: confidential';
-- Set comment to column: "status" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."status" IS '@access: confidential';
-- Set comment to column: "response" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."response" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "killswitch_operations"
COMMENT ON COLUMN "killswitch_operations"."updated_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "killswitch_prescription_version_resources"
COMMENT ON COLUMN "killswitch_prescription_version_resources"."organization_id" IS '@access: confidential';
-- Set comment to column: "prescription_id" on table: "killswitch_prescription_version_resources"
COMMENT ON COLUMN "killswitch_prescription_version_resources"."prescription_id" IS '@access: confidential';
-- Set comment to column: "version" on table: "killswitch_prescription_version_resources"
COMMENT ON COLUMN "killswitch_prescription_version_resources"."version" IS '@access: confidential';
-- Set comment to column: "resource_key" on table: "killswitch_prescription_version_resources"
COMMENT ON COLUMN "killswitch_prescription_version_resources"."resource_key" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."organization_id" IS '@access: confidential';
-- Set comment to column: "prescription_id" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."prescription_id" IS '@access: confidential';
-- Set comment to column: "version" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."version" IS '@access: confidential';
-- Set comment to column: "state" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."state" IS '@access: confidential';
-- Set comment to column: "resource_scope" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."resource_scope" IS '@access: confidential';
-- Set comment to column: "starts_at" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."starts_at" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."expires_at" IS '@access: confidential';
-- Set comment to column: "activated_at" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."activated_at" IS '@access: confidential';
-- Set comment to column: "superseded_at" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."superseded_at" IS '@access: confidential';
-- Set comment to column: "internal_note" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."internal_note" IS '@access: opaque-restricted';
-- Set comment to column: "external_note" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."external_note" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "killswitch_prescription_versions"
COMMENT ON COLUMN "killswitch_prescription_versions"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."organization_id" IS '@access: confidential';
-- Set comment to column: "definition_key" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."definition_key" IS '@access: confidential';
-- Set comment to column: "principal_kind" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."principal_kind" IS '@access: confidential';
-- Set comment to column: "principal_key" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."principal_key" IS '@access: confidential';
-- Set comment to column: "resource_kind" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."resource_kind" IS '@access: confidential';
-- Set comment to column: "current_version" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."current_version" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "killswitch_prescriptions"
COMMENT ON COLUMN "killswitch_prescriptions"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."project_id" IS '@access: confidential';
-- Set comment to column: "api_key_id" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."api_key_id" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."name" IS '@access: confidential';
-- Set comment to column: "failure_posture" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."failure_posture" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."deleted" IS '@access: confidential';
-- Set comment to column: "last_guardrail_event_at" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."last_guardrail_event_at" IS '@access: confidential';
-- Set comment to column: "last_otel_event_at" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."last_otel_event_at" IS '@access: confidential';
-- Set comment to column: "last_error_at" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."last_error_at" IS '@access: confidential';
-- Set comment to column: "last_error_kind" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."last_error_kind" IS '@access: confidential';
-- Set comment to column: "reported_litellm_version" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."reported_litellm_version" IS '@access: opaque-restricted';
-- Set comment to column: "reported_litellm_version_at" on table: "litellm_instances"
COMMENT ON COLUMN "litellm_instances"."reported_litellm_version_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."project_id" IS '@access: confidential';
-- Set comment to column: "mcp_approval_request_id" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."mcp_approval_request_id" IS '@access: confidential';
-- Set comment to column: "decision" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."decision" IS '@access: confidential';
-- Set comment to column: "decided_by" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."decided_by" IS '@access: confidential';
-- Set comment to column: "rationale" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."rationale" IS '@access: opaque-restricted';
-- Set comment to column: "evidence_snapshot" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."evidence_snapshot" IS '@access: opaque-restricted';
-- Set comment to column: "evidence_version" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."evidence_version" IS '@access: confidential';
-- Set comment to column: "mcp_research_report_id" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."mcp_research_report_id" IS '@access: confidential';
-- Set comment to column: "granted_principal_urns" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."granted_principal_urns" IS 'Resolved blast radius of the approval. Empty for a denial.
@access: confidential';
-- Set comment to column: "decided_at" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."decided_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_approval_decisions"
COMMENT ON COLUMN "mcp_approval_decisions"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."project_id" IS '@access: confidential';
-- Set comment to column: "mcp_approval_request_id" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."mcp_approval_request_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."user_id" IS '@access: confidential';
-- Set comment to column: "user_email" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."user_email" IS '@access: confidential-pii';
-- Set comment to column: "note" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."note" IS '@access: opaque-restricted';
-- Set comment to column: "requested_at" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."requested_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_approval_request_requesters"
COMMENT ON COLUMN "mcp_approval_request_requesters"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."project_id" IS '@access: confidential';
-- Set comment to column: "target_kind" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."target_kind" IS '@access: confidential';
-- Set comment to column: "target_raw" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."target_raw" IS '@access: opaque-restricted';
-- Set comment to column: "target_key" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."target_key" IS '@access: opaque-restricted';
-- Set comment to column: "artifact_ref" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."artifact_ref" IS 'Resolved immutable artifact identity. NULL means unidentified, which must surface as unknown rather than as an absence of findings.
@access: restricted';
-- Set comment to column: "version_pinned" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."version_pinned" IS 'False for a floating invocation such as an unpinned npx command, where anything scanned may not be what runs.
@access: confidential';
-- Set comment to column: "risk_policy_bypass_request_id" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."risk_policy_bypass_request_id" IS '@access: confidential';
-- Set comment to column: "status" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."status" IS '@access: confidential';
-- Set comment to column: "current_evidence" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."current_evidence" IS '@access: opaque-restricted';
-- Set comment to column: "evidence_version" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."evidence_version" IS '@access: confidential';
-- Set comment to column: "evidence_collected_at" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."evidence_collected_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."deleted" IS '@access: confidential';
-- Set comment to column: "evidence_changed_at" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."evidence_changed_at" IS '@access: confidential';
-- Set comment to column: "notified_change_fingerprint" on table: "mcp_approval_requests"
COMMENT ON COLUMN "mcp_approval_requests"."notified_change_fingerprint" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."project_id" IS '@access: confidential';
-- Set comment to column: "custom_domain_id" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."custom_domain_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."slug" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."deleted" IS '@access: confidential';
-- Set comment to column: "is_domain_root" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."is_domain_root" IS '@access: confidential';
-- Set comment to column: "meta_mcp_server_id" on table: "mcp_endpoints"
COMMENT ON COLUMN "mcp_endpoints"."meta_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_environment_configs"
COMMENT ON COLUMN "mcp_environment_configs"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_environment_configs"
COMMENT ON COLUMN "mcp_environment_configs"."project_id" IS '@access: confidential';
-- Set comment to column: "mcp_metadata_id" on table: "mcp_environment_configs"
COMMENT ON COLUMN "mcp_environment_configs"."mcp_metadata_id" IS '@access: confidential';
-- Set comment to column: "variable_name" on table: "mcp_environment_configs"
COMMENT ON COLUMN "mcp_environment_configs"."variable_name" IS '@access: confidential';
-- Set comment to column: "header_display_name" on table: "mcp_environment_configs"
COMMENT ON COLUMN "mcp_environment_configs"."header_display_name" IS '@access: confidential';
-- Set comment to column: "provided_by" on table: "mcp_environment_configs"
COMMENT ON COLUMN "mcp_environment_configs"."provided_by" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "mcp_environment_configs"
COMMENT ON COLUMN "mcp_environment_configs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_environment_configs"
COMMENT ON COLUMN "mcp_environment_configs"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."toolset_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."project_id" IS '@access: confidential';
-- Set comment to column: "external_documentation_url" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."external_documentation_url" IS '@access: restricted';
-- Set comment to column: "logo_id" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."logo_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."updated_at" IS '@access: confidential';
-- Set comment to column: "instructions" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."instructions" IS '@access: opaque-restricted';
-- Set comment to column: "header_display_names" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."header_display_names" IS '@access: opaque-restricted';
-- Set comment to column: "default_environment_id" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."default_environment_id" IS '@access: confidential';
-- Set comment to column: "installation_override_url" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."installation_override_url" IS '@access: restricted';
-- Set comment to column: "external_documentation_text" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."external_documentation_text" IS '@access: opaque-restricted';
-- Set comment to column: "mcp_server_id" on table: "mcp_metadata"
COMMENT ON COLUMN "mcp_metadata"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."id" IS '@access: confidential';
-- Set comment to column: "name" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."name" IS '@access: confidential';
-- Set comment to column: "url" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."url" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."deleted" IS '@access: confidential';
-- Set comment to column: "source_type" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."source_type" IS '@access: confidential';
-- Set comment to column: "auth_profile" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."auth_profile" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."enabled" IS '@access: confidential';
-- Set comment to column: "certification_state" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."certification_state" IS '@access: confidential';
-- Set comment to column: "certification_version" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."certification_version" IS '@access: confidential';
-- Set comment to column: "priority" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."priority" IS '@access: confidential';
-- Set comment to column: "source_key" on table: "mcp_registries"
COMMENT ON COLUMN "mcp_registries"."source_key" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_registry_entries"
COMMENT ON COLUMN "mcp_registry_entries"."id" IS '@access: confidential';
-- Set comment to column: "data" on table: "mcp_registry_entries"
COMMENT ON COLUMN "mcp_registry_entries"."data" IS '@access: opaque-restricted';
-- Set comment to column: "published" on table: "mcp_registry_entries"
COMMENT ON COLUMN "mcp_registry_entries"."published" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_registry_entries"
COMMENT ON COLUMN "mcp_registry_entries"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_registry_entries"
COMMENT ON COLUMN "mcp_registry_entries"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."project_id" IS '@access: confidential';
-- Set comment to column: "mcp_approval_request_id" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."mcp_approval_request_id" IS '@access: confidential';
-- Set comment to column: "status" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."status" IS '@access: confidential';
-- Set comment to column: "report" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."report" IS '@access: opaque-restricted';
-- Set comment to column: "report_version" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."report_version" IS '@access: confidential';
-- Set comment to column: "model" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."model" IS '@access: confidential';
-- Set comment to column: "prompt_version" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."prompt_version" IS '@access: confidential';
-- Set comment to column: "requested_by" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."requested_by" IS '@access: confidential';
-- Set comment to column: "started_at" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."started_at" IS '@access: confidential';
-- Set comment to column: "completed_at" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."completed_at" IS '@access: confidential';
-- Set comment to column: "error" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."error" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."deleted" IS '@access: confidential';
-- Set comment to column: "tool_calls" on table: "mcp_research_reports"
COMMENT ON COLUMN "mcp_research_reports"."tool_calls" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."project_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "tool_name" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."tool_name" IS '@access: confidential';
-- Set comment to column: "title" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."title" IS '@access: confidential';
-- Set comment to column: "read_only_hint" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."read_only_hint" IS '@access: confidential';
-- Set comment to column: "destructive_hint" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."destructive_hint" IS '@access: confidential';
-- Set comment to column: "idempotent_hint" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."idempotent_hint" IS '@access: confidential';
-- Set comment to column: "open_world_hint" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."open_world_hint" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "environment_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."environment_id" IS '@access: confidential';
-- Set comment to column: "remote_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."remote_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."toolset_id" IS '@access: confidential';
-- Set comment to column: "visibility" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."visibility" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."deleted" IS '@access: confidential';
-- Set comment to column: "name" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."name" IS '@access: restricted';
-- Set comment to column: "slug" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."slug" IS '@access: restricted';
-- Set comment to column: "user_session_issuer_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "tool_variations_group_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tool_variations_group_id" IS '@access: confidential';
-- Set comment to column: "tunneled_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tunneled_mcp_server_id" IS 'Optional backend reference to a tunneled MCP source. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, or unproxied_mcp_server_id must be set.
@access: confidential';
-- Set comment to column: "unproxied_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."unproxied_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "remote_session_issuer_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."remote_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "network_access_mode" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."network_access_mode" IS '@access: confidential';
-- Set comment to column: "id" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."id" IS '@access: confidential';
-- Set comment to column: "device_integration_config_id" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."device_integration_config_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."organization_id" IS '@access: confidential';
-- Set comment to column: "external_id" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."external_id" IS '@access: confidential';
-- Set comment to column: "serial_number" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."serial_number" IS '@access: confidential-pii';
-- Set comment to column: "hostname" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."hostname" IS '@access: confidential-pii';
-- Set comment to column: "os_name" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."os_name" IS '@access: confidential';
-- Set comment to column: "os_version" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."os_version" IS '@access: confidential';
-- Set comment to column: "user_email" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."user_email" IS '@access: confidential-pii';
-- Set comment to column: "user_id" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."user_id" IS '@access: confidential';
-- Set comment to column: "mdm_last_check_in_at" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."mdm_last_check_in_at" IS '@access: confidential';
-- Set comment to column: "raw" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."raw" IS '@access: opaque-restricted';
-- Set comment to column: "first_seen_at" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "missing_since" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."missing_since" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mdm_devices"
COMMENT ON COLUMN "mdm_devices"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."project_id" IS '@access: confidential';
-- Set comment to column: "meta_mcp_server_id" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."meta_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "sort_order" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."sort_order" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "meta_mcp_server_members"
COMMENT ON COLUMN "meta_mcp_server_members"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."deleted" IS '@access: confidential';
-- Set comment to column: "visibility" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."visibility" IS '@access: confidential';
-- Set comment to column: "network_access_mode" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."network_access_mode" IS '@access: confidential';
-- Set comment to column: "instructions" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."instructions" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."project_id" IS '@access: confidential';
-- Set comment to column: "slot" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."slot" IS '@access: confidential';
-- Set comment to column: "provider" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."provider" IS '@access: confidential';
-- Set comment to column: "api_key_encrypted" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."api_key_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "enabled" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."enabled" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."organization_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."provider" IS '@access: confidential';
-- Set comment to column: "hostname" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."hostname" IS '@access: restricted';
-- Set comment to column: "endpoint_namespace_kind" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."endpoint_namespace_kind" IS '@access: confidential';
-- Set comment to column: "custom_domain_id" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."custom_domain_id" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."enabled" IS '@access: confidential';
-- Set comment to column: "identity_required" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."identity_required" IS '@access: confidential';
-- Set comment to column: "credentials_encrypted" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."credentials_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "attestor_namespace" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."attestor_namespace" IS '@access: confidential';
-- Set comment to column: "attestor_service_account" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."attestor_service_account" IS '@access: confidential';
-- Set comment to column: "provider_resources" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."provider_resources" IS '@access: confidential';
-- Set comment to column: "status" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."status" IS '@access: confidential';
-- Set comment to column: "dns_name" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."dns_name" IS '@access: restricted';
-- Set comment to column: "last_error" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."last_error" IS '@access: confidential';
-- Set comment to column: "health_checked_at" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."health_checked_at" IS '@access: confidential';
-- Set comment to column: "connected_since" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."connected_since" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."deleted" IS '@access: confidential';
-- Set comment to column: "mcp_slug" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."mcp_slug" IS '@access: confidential';
-- Set comment to column: "client_id" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."client_id" IS '@access: confidential';
-- Set comment to column: "client_secret" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."client_secret" IS '@access: secret-restricted';
-- Set comment to column: "client_secret_expires_at" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."client_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "client_name" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."client_name" IS '@access: opaque-restricted';
-- Set comment to column: "redirect_uris" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."redirect_uris" IS '@access: opaque-restricted';
-- Set comment to column: "grant_types" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."grant_types" IS '@access: confidential';
-- Set comment to column: "response_types" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."response_types" IS '@access: confidential';
-- Set comment to column: "scope" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."scope" IS '@access: opaque-restricted';
-- Set comment to column: "token_endpoint_auth_method" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."token_endpoint_auth_method" IS '@access: confidential';
-- Set comment to column: "application_type" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."application_type" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "oauth_proxy_client_info"
COMMENT ON COLUMN "oauth_proxy_client_info"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."project_id" IS '@access: confidential';
-- Set comment to column: "oauth_proxy_server_id" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."oauth_proxy_server_id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."slug" IS '@access: confidential';
-- Set comment to column: "authorization_endpoint" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."authorization_endpoint" IS '@access: restricted';
-- Set comment to column: "token_endpoint" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."token_endpoint" IS '@access: restricted';
-- Set comment to column: "registration_endpoint" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."registration_endpoint" IS '@access: restricted';
-- Set comment to column: "scopes_supported" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."scopes_supported" IS '@access: opaque-restricted';
-- Set comment to column: "response_types_supported" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."response_types_supported" IS '@access: confidential';
-- Set comment to column: "response_modes_supported" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."response_modes_supported" IS '@access: confidential';
-- Set comment to column: "grant_types_supported" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."grant_types_supported" IS '@access: confidential';
-- Set comment to column: "token_endpoint_auth_methods_supported" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."token_endpoint_auth_methods_supported" IS '@access: opaque-restricted';
-- Set comment to column: "security_key_names" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."security_key_names" IS '@access: confidential';
-- Set comment to column: "secrets" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."secrets" IS '@access: secret-restricted';
-- Set comment to column: "created_at" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."deleted" IS '@access: confidential';
-- Set comment to column: "provider_type" on table: "oauth_proxy_providers"
COMMENT ON COLUMN "oauth_proxy_providers"."provider_type" IS '@access: confidential';
-- Set comment to column: "id" on table: "oauth_proxy_servers"
COMMENT ON COLUMN "oauth_proxy_servers"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "oauth_proxy_servers"
COMMENT ON COLUMN "oauth_proxy_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "oauth_proxy_servers"
COMMENT ON COLUMN "oauth_proxy_servers"."slug" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "oauth_proxy_servers"
COMMENT ON COLUMN "oauth_proxy_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "oauth_proxy_servers"
COMMENT ON COLUMN "oauth_proxy_servers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "oauth_proxy_servers"
COMMENT ON COLUMN "oauth_proxy_servers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "oauth_proxy_servers"
COMMENT ON COLUMN "oauth_proxy_servers"."deleted" IS '@access: confidential';
-- Set comment to column: "audience" on table: "oauth_proxy_servers"
COMMENT ON COLUMN "oauth_proxy_servers"."audience" IS '@access: restricted';
-- Set comment to column: "id" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."organization_id" IS '@access: confidential';
-- Set comment to column: "identity_provider_connection_id" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."identity_provider_connection_id" IS '@access: confidential';
-- Set comment to column: "okta_app_id" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."okta_app_id" IS '@access: confidential';
-- Set comment to column: "principal_kind" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."principal_kind" IS '@access: confidential';
-- Set comment to column: "okta_principal_id" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."okta_principal_id" IS '@access: confidential-pii';
-- Set comment to column: "assignment_scope" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."assignment_scope" IS '@access: confidential';
-- Set comment to column: "first_seen_at" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "removed_at" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."removed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "okta_application_assignments"
COMMENT ON COLUMN "okta_application_assignments"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."organization_id" IS '@access: confidential';
-- Set comment to column: "identity_provider_connection_id" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."identity_provider_connection_id" IS '@access: confidential';
-- Set comment to column: "status" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."status" IS '@access: confidential';
-- Set comment to column: "started_at" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."started_at" IS '@access: confidential';
-- Set comment to column: "finished_at" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."finished_at" IS '@access: confidential';
-- Set comment to column: "applications_seen" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."applications_seen" IS '@access: confidential';
-- Set comment to column: "applications_added" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."applications_added" IS '@access: confidential';
-- Set comment to column: "applications_removed" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."applications_removed" IS '@access: confidential';
-- Set comment to column: "assignments_added" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."assignments_added" IS '@access: confidential';
-- Set comment to column: "assignments_removed" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."assignments_removed" IS '@access: confidential';
-- Set comment to column: "skipped_app_ids" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."skipped_app_ids" IS '@access: confidential';
-- Set comment to column: "truncated" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."truncated" IS '@access: confidential';
-- Set comment to column: "error" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."error" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "okta_application_reconcile_runs"
COMMENT ON COLUMN "okta_application_reconcile_runs"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."organization_id" IS '@access: confidential';
-- Set comment to column: "identity_provider_connection_id" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."identity_provider_connection_id" IS '@access: confidential';
-- Set comment to column: "okta_app_id" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."okta_app_id" IS '@access: confidential';
-- Set comment to column: "label" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."label" IS '@access: confidential';
-- Set comment to column: "name" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."name" IS '@access: confidential';
-- Set comment to column: "sign_on_mode" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."sign_on_mode" IS '@access: confidential';
-- Set comment to column: "status" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."status" IS '@access: confidential';
-- Set comment to column: "features" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."features" IS '@access: opaque-restricted';
-- Set comment to column: "okta_created_at" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."okta_created_at" IS '@access: confidential';
-- Set comment to column: "okta_last_updated_at" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."okta_last_updated_at" IS '@access: confidential';
-- Set comment to column: "first_seen_at" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "removed_at" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."removed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "okta_applications"
COMMENT ON COLUMN "okta_applications"."updated_at" IS '@access: confidential';
-- Set comment to column: "identity_provider_connection_id" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."identity_provider_connection_id" IS '@access: confidential';
-- Set comment to column: "identity_provider_connections_provider" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."identity_provider_connections_provider" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."organization_id" IS '@access: confidential';
-- Set comment to column: "attachment_scope" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."attachment_scope" IS '@access: confidential';
-- Set comment to column: "org_url" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."org_url" IS '@access: restricted';
-- Set comment to column: "issuer_url" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."issuer_url" IS '@access: restricted';
-- Set comment to column: "issuer_url_override_reason" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."issuer_url_override_reason" IS '@access: opaque-restricted';
-- Set comment to column: "ownership_claimed" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."ownership_claimed" IS '@access: confidential';
-- Set comment to column: "remote_session_issuer_id" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."remote_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "remote_session_client_id" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "dpop_required" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."dpop_required" IS '@access: confidential';
-- Set comment to column: "granted_scopes" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."granted_scopes" IS '@access: opaque-restricted';
-- Set comment to column: "observed_admin_roles" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."observed_admin_roles" IS '@access: confidential';
-- Set comment to column: "listing_mode" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."listing_mode" IS '@access: confidential';
-- Set comment to column: "agent_id" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."agent_id" IS '@access: confidential';
-- Set comment to column: "agent_app_id" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."agent_app_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."deleted" IS '@access: confidential';
-- Set comment to column: "applications_synced_at" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."applications_synced_at" IS '@access: confidential';
-- Set comment to column: "applications_sync_requested_at" on table: "okta_identity_provider_connections"
COMMENT ON COLUMN "okta_identity_provider_connections"."applications_sync_requested_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."organization_id" IS '@access: confidential';
-- Set comment to column: "identity_provider_connection_id" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."identity_provider_connection_id" IS '@access: confidential';
-- Set comment to column: "remote_session_issuer_id" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."remote_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "resource" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."resource" IS '@access: restricted';
-- Set comment to column: "audience" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."audience" IS '@access: restricted';
-- Set comment to column: "okta_application_id" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."okta_application_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "okta_resource_connections"
COMMENT ON COLUMN "okta_resource_connections"."updated_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."organization_id" IS '@access: confidential';
-- Set comment to column: "key" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."key" IS '@access: secret-restricted';
-- Set comment to column: "key_hash" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."key_hash" IS '@access: secret-restricted';
-- Set comment to column: "created_at" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."deleted" IS '@access: confidential';
-- Set comment to column: "monthly_credits" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."monthly_credits" IS '@access: restricted';
-- Set comment to column: "disabled" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."disabled" IS '@access: confidential';
-- Set comment to column: "key_type" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."key_type" IS '@access: confidential';
-- Set comment to column: "key_encrypted" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."key_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "disable_causes" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."disable_causes" IS '@access: confidential';
-- Set comment to column: "id" on table: "openrouter_spend_daily"
COMMENT ON COLUMN "openrouter_spend_daily"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "openrouter_spend_daily"
COMMENT ON COLUMN "openrouter_spend_daily"."organization_id" IS '@access: confidential';
-- Set comment to column: "key_type" on table: "openrouter_spend_daily"
COMMENT ON COLUMN "openrouter_spend_daily"."key_type" IS '@access: confidential';
-- Set comment to column: "day" on table: "openrouter_spend_daily"
COMMENT ON COLUMN "openrouter_spend_daily"."day" IS '@access: confidential';
-- Set comment to column: "spend_usd" on table: "openrouter_spend_daily"
COMMENT ON COLUMN "openrouter_spend_daily"."spend_usd" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "openrouter_spend_daily"
COMMENT ON COLUMN "openrouter_spend_daily"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "openrouter_spend_daily"
COMMENT ON COLUMN "openrouter_spend_daily"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_features"
COMMENT ON COLUMN "organization_features"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_features"
COMMENT ON COLUMN "organization_features"."organization_id" IS '@access: confidential';
-- Set comment to column: "feature_name" on table: "organization_features"
COMMENT ON COLUMN "organization_features"."feature_name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_features"
COMMENT ON COLUMN "organization_features"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_features"
COMMENT ON COLUMN "organization_features"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "organization_features"
COMMENT ON COLUMN "organization_features"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "organization_features"
COMMENT ON COLUMN "organization_features"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."organization_id" IS '@access: confidential';
-- Set comment to column: "email" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."email" IS '@access: confidential-pii';
-- Set comment to column: "token_hash" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."token_hash" IS '@access: secret-restricted';
-- Set comment to column: "inviter_user_id" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."inviter_user_id" IS '@access: confidential';
-- Set comment to column: "role_slug" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."role_slug" IS '@access: confidential';
-- Set comment to column: "state" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."state" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."expires_at" IS '@access: confidential';
-- Set comment to column: "accepted_at" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."accepted_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_invitations"
COMMENT ON COLUMN "organization_invitations"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_mcp_collection_registries"
COMMENT ON COLUMN "organization_mcp_collection_registries"."id" IS '@access: confidential';
-- Set comment to column: "collection_id" on table: "organization_mcp_collection_registries"
COMMENT ON COLUMN "organization_mcp_collection_registries"."collection_id" IS '@access: confidential';
-- Set comment to column: "namespace" on table: "organization_mcp_collection_registries"
COMMENT ON COLUMN "organization_mcp_collection_registries"."namespace" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_mcp_collection_registries"
COMMENT ON COLUMN "organization_mcp_collection_registries"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_mcp_collection_registries"
COMMENT ON COLUMN "organization_mcp_collection_registries"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "organization_mcp_collection_registries"
COMMENT ON COLUMN "organization_mcp_collection_registries"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "organization_mcp_collection_registries"
COMMENT ON COLUMN "organization_mcp_collection_registries"."deleted" IS '@access: confidential';
-- Set comment to column: "published_at" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."published_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."deleted_at" IS '@access: confidential';
-- Set comment to column: "published_by" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."published_by" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."id" IS '@access: confidential';
-- Set comment to column: "collection_id" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."collection_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."toolset_id" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."deleted" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "organization_mcp_collection_server_attachments"
COMMENT ON COLUMN "organization_mcp_collection_server_attachments"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."organization_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."description" IS '@access: opaque-restricted';
-- Set comment to column: "slug" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."slug" IS '@access: confidential';
-- Set comment to column: "visibility" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."visibility" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "organization_mcp_collections"
COMMENT ON COLUMN "organization_mcp_collections"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."id" IS '@access: confidential';
-- Set comment to column: "name" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."slug" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."updated_at" IS '@access: confidential';
-- Set comment to column: "gram_account_type" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."gram_account_type" IS '@access: confidential';
-- Set comment to column: "disabled_at" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."disabled_at" IS '@access: confidential';
-- Set comment to column: "workos_id" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."workos_id" IS '@access: confidential';
-- Set comment to column: "free_trial_started_at" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."free_trial_started_at" IS '@access: confidential';
-- Set comment to column: "free_trial_ends_at" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."free_trial_ends_at" IS '@access: confidential';
-- Set comment to column: "whitelisted" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."whitelisted" IS '@access: confidential';
-- Set comment to column: "workos_updated_at" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."workos_updated_at" IS '@access: confidential';
-- Set comment to column: "workos_last_event_id" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."workos_last_event_id" IS '@access: confidential';
-- Set comment to column: "svix_app_id" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."svix_app_id" IS '@access: confidential';
-- Set comment to column: "webhooks_enabled" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."webhooks_enabled" IS '@access: confidential';
-- Set comment to column: "scim_enabled" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."scim_enabled" IS '@access: confidential';
-- Set comment to column: "sso_enabled" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."sso_enabled" IS '@access: confidential';
-- Set comment to column: "creation_source" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."creation_source" IS '@access: confidential';
-- Set comment to column: "verified_domains" on table: "organization_metadata"
COMMENT ON COLUMN "organization_metadata"."verified_domains" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "organization_onboarding"
COMMENT ON COLUMN "organization_onboarding"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_onboarding"
COMMENT ON COLUMN "organization_onboarding"."organization_id" IS '@access: confidential';
-- Set comment to column: "preset" on table: "organization_onboarding"
COMMENT ON COLUMN "organization_onboarding"."preset" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_onboarding"
COMMENT ON COLUMN "organization_onboarding"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_onboarding"
COMMENT ON COLUMN "organization_onboarding"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."organization_id" IS '@access: confidential';
-- Set comment to column: "workos_user_id" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."workos_user_id" IS '@access: confidential-pii';
-- Set comment to column: "user_id" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."user_id" IS '@access: confidential';
-- Set comment to column: "role_urn" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."role_urn" IS '@access: confidential';
-- Set comment to column: "workos_membership_id" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."workos_membership_id" IS '@access: confidential-pii';
-- Set comment to column: "workos_updated_at" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."workos_updated_at" IS '@access: confidential';
-- Set comment to column: "workos_last_event_id" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."workos_last_event_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "organization_role_assignments"
COMMENT ON COLUMN "organization_role_assignments"."deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."organization_id" IS '@access: confidential';
-- Set comment to column: "workos_slug" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."workos_slug" IS '@access: confidential';
-- Set comment to column: "workos_name" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."workos_name" IS '@access: confidential';
-- Set comment to column: "workos_description" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."workos_description" IS '@access: opaque-restricted';
-- Set comment to column: "workos_created_at" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."workos_created_at" IS '@access: confidential';
-- Set comment to column: "workos_updated_at" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."workos_updated_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted_at" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."workos_deleted_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."workos_deleted" IS '@access: confidential';
-- Set comment to column: "workos_last_event_id" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."workos_last_event_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "organization_roles"
COMMENT ON COLUMN "organization_roles"."deleted" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_setup_tasks"
COMMENT ON COLUMN "organization_setup_tasks"."organization_id" IS '@access: confidential';
-- Set comment to column: "task_key" on table: "organization_setup_tasks"
COMMENT ON COLUMN "organization_setup_tasks"."task_key" IS '@access: confidential';
-- Set comment to column: "status" on table: "organization_setup_tasks"
COMMENT ON COLUMN "organization_setup_tasks"."status" IS '@access: confidential';
-- Set comment to column: "assignee_user_id" on table: "organization_setup_tasks"
COMMENT ON COLUMN "organization_setup_tasks"."assignee_user_id" IS '@access: confidential';
-- Set comment to column: "assignee_email" on table: "organization_setup_tasks"
COMMENT ON COLUMN "organization_setup_tasks"."assignee_email" IS '@access: confidential-pii';
-- Set comment to column: "hidden_at" on table: "organization_setup_tasks"
COMMENT ON COLUMN "organization_setup_tasks"."hidden_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_setup_tasks"
COMMENT ON COLUMN "organization_setup_tasks"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_setup_tasks"
COMMENT ON COLUMN "organization_setup_tasks"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."organization_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."user_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."deleted" IS '@access: confidential';
-- Set comment to column: "workos_membership_id" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."workos_membership_id" IS '@access: confidential-pii';
-- Set comment to column: "workos_updated_at" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."workos_updated_at" IS '@access: confidential';
-- Set comment to column: "workos_last_event_id" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."workos_last_event_id" IS '@access: confidential';
-- Set comment to column: "workos_user_id" on table: "organization_user_relationships"
COMMENT ON COLUMN "organization_user_relationships"."workos_user_id" IS '@access: confidential-pii';
-- Set comment to column: "id" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."project_id" IS '@access: confidential';
-- Set comment to column: "endpoint_url" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."endpoint_url" IS '@access: restricted';
-- Set comment to column: "headers_encrypted" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."headers_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "sensitive_data" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."sensitive_data" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."deleted" IS '@access: confidential';
-- Set comment to column: "name" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."created_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."deleted_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."updated_at" IS '@access: confidential';
-- Set comment to column: "endpoint_url" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."endpoint_url" IS '@access: restricted';
-- Set comment to column: "headers_encrypted" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."headers_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "organization_id" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."project_id" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."enabled" IS '@access: confidential';
-- Set comment to column: "id" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."id" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "outbox"
COMMENT ON COLUMN "outbox"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "outbox"
COMMENT ON COLUMN "outbox"."organization_id" IS '@access: confidential';
-- Set comment to column: "event_type" on table: "outbox"
COMMENT ON COLUMN "outbox"."event_type" IS '@access: confidential';
-- Set comment to column: "payload" on table: "outbox"
COMMENT ON COLUMN "outbox"."payload" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "outbox"
COMMENT ON COLUMN "outbox"."created_at" IS '@access: confidential';
-- Set comment to column: "public_id" on table: "outbox"
COMMENT ON COLUMN "outbox"."public_id" IS '@access: confidential';
-- Set comment to column: "outbox_id" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."outbox_id" IS '@access: confidential';
-- Set comment to column: "processed_at" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."processed_at" IS '@access: confidential';
-- Set comment to column: "noop" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."noop" IS '@access: confidential';
-- Set comment to column: "dead_lettered" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."dead_lettered" IS '@access: confidential';
-- Set comment to column: "svix_message_id" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."svix_message_id" IS '@access: confidential';
-- Set comment to column: "attempts" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."attempts" IS '@access: confidential';
-- Set comment to column: "last_error" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."last_error" IS '@access: opaque-restricted';
-- Set comment to column: "retry_after" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."retry_after" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "outbox_relays"
COMMENT ON COLUMN "outbox_relays"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."id" IS '@access: confidential';
-- Set comment to column: "package_id" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."package_id" IS '@access: confidential';
-- Set comment to column: "deployment_id" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."deployment_id" IS '@access: confidential';
-- Set comment to column: "visibility" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."visibility" IS '@access: confidential';
-- Set comment to column: "major" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."major" IS '@access: confidential';
-- Set comment to column: "minor" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."minor" IS '@access: confidential';
-- Set comment to column: "patch" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."patch" IS '@access: confidential';
-- Set comment to column: "prerelease" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."prerelease" IS '@access: confidential';
-- Set comment to column: "build" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."build" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "package_versions"
COMMENT ON COLUMN "package_versions"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "packages"
COMMENT ON COLUMN "packages"."id" IS '@access: confidential';
-- Set comment to column: "name" on table: "packages"
COMMENT ON COLUMN "packages"."name" IS '@access: confidential';
-- Set comment to column: "title" on table: "packages"
COMMENT ON COLUMN "packages"."title" IS '@access: confidential';
-- Set comment to column: "summary" on table: "packages"
COMMENT ON COLUMN "packages"."summary" IS '@access: opaque-restricted';
-- Set comment to column: "keywords" on table: "packages"
COMMENT ON COLUMN "packages"."keywords" IS '@access: opaque-restricted';
-- Set comment to column: "organization_id" on table: "packages"
COMMENT ON COLUMN "packages"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "packages"
COMMENT ON COLUMN "packages"."project_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "packages"
COMMENT ON COLUMN "packages"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "packages"
COMMENT ON COLUMN "packages"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "packages"
COMMENT ON COLUMN "packages"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "packages"
COMMENT ON COLUMN "packages"."deleted" IS '@access: confidential';
-- Set comment to column: "image_asset_id" on table: "packages"
COMMENT ON COLUMN "packages"."image_asset_id" IS '@access: confidential';
-- Set comment to column: "url" on table: "packages"
COMMENT ON COLUMN "packages"."url" IS '@access: restricted';
-- Set comment to column: "description_raw" on table: "packages"
COMMENT ON COLUMN "packages"."description_raw" IS '@access: opaque-restricted';
-- Set comment to column: "description_html" on table: "packages"
COMMENT ON COLUMN "packages"."description_html" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."organization_id" IS '@access: confidential';
-- Set comment to column: "authorization_code_hash" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."authorization_code_hash" IS '@access: secret-restricted';
-- Set comment to column: "oauth_client_id" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."connection_generation" IS '@access: confidential';
-- Set comment to column: "redirect_uri" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."redirect_uri" IS '@access: restricted';
-- Set comment to column: "code_challenge" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."code_challenge" IS '@access: secret-restricted';
-- Set comment to column: "expires_at" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."expires_at" IS '@access: confidential';
-- Set comment to column: "consumed_at" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."consumed_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_authorization_grants"
COMMENT ON COLUMN "platform_mcp_authorization_grants"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."project_id" IS '@access: confidential';
-- Set comment to column: "source_kind" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."source_kind" IS '@access: confidential';
-- Set comment to column: "catalog_provider" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."catalog_provider" IS '@access: confidential';
-- Set comment to column: "catalog_reference" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."catalog_reference" IS '@access: restricted';
-- Set comment to column: "status" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."status" IS '@access: confidential';
-- Set comment to column: "remote_mcp_server_id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."remote_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "remote_mcp_server_owned" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."remote_mcp_server_owned" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_owned" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."user_session_issuer_owned" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_owned" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."mcp_server_owned" IS '@access: confidential';
-- Set comment to column: "mcp_endpoint_id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."mcp_endpoint_id" IS '@access: confidential';
-- Set comment to column: "mcp_endpoint_owned" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."mcp_endpoint_owned" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."connection_generation" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."deleted" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."user_id" IS '@access: confidential';
-- Set comment to column: "acting_surface" on table: "platform_mcp_catalog_registrations"
COMMENT ON COLUMN "platform_mcp_catalog_registrations"."acting_surface" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."organization_id" IS '@access: confidential';
-- Set comment to column: "subject_urn" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."subject_urn" IS '@access: confidential';
-- Set comment to column: "oauth_client_id" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "active_generation" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."active_generation" IS '@access: confidential';
-- Set comment to column: "authorized_at" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."authorized_at" IS '@access: confidential';
-- Set comment to column: "reauthorized_at" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."reauthorized_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."updated_at" IS '@access: confidential';
-- Set comment to column: "authorization_expires_at" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."authorization_expires_at" IS '@access: confidential';
-- Set comment to column: "reauthorization_required_at" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."reauthorization_required_at" IS '@access: confidential';
-- Set comment to column: "reauthorization_reason" on table: "platform_mcp_connections"
COMMENT ON COLUMN "platform_mcp_connections"."reauthorization_reason" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."project_id" IS '@access: confidential';
-- Set comment to column: "registration_id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."registration_id" IS '@access: confidential';
-- Set comment to column: "default_plugin_id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."default_plugin_id" IS '@access: confidential';
-- Set comment to column: "plugin_server_id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."plugin_server_id" IS '@access: confidential';
-- Set comment to column: "state" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."state" IS '@access: confidential';
-- Set comment to column: "version" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."version" IS '@access: confidential';
-- Set comment to column: "attachment_was_created" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."attachment_was_created" IS '@access: confidential';
-- Set comment to column: "publication_state" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."publication_state" IS '@access: confidential';
-- Set comment to column: "publication_updated_at" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."publication_updated_at" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."connection_generation" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."updated_at" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."user_id" IS '@access: confidential';
-- Set comment to column: "acting_surface" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."acting_surface" IS '@access: confidential';
-- Set comment to column: "plugin_id" on table: "platform_mcp_distributions"
COMMENT ON COLUMN "platform_mcp_distributions"."plugin_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."organization_id" IS '@access: confidential';
-- Set comment to column: "subject_urn" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."subject_urn" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."connection_generation" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."project_id" IS '@access: confidential';
-- Set comment to column: "workflow_id" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."workflow_id" IS '@access: confidential';
-- Set comment to column: "request_reference" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."request_reference" IS '@access: restricted';
-- Set comment to column: "category" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."category" IS '@access: confidential';
-- Set comment to column: "idempotency_key" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."idempotency_key" IS '@access: opaque-restricted';
-- Set comment to column: "input_hash" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."input_hash" IS '@access: confidential';
-- Set comment to column: "rating" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."rating" IS '@access: confidential';
-- Set comment to column: "success" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."success" IS '@access: confidential';
-- Set comment to column: "tool_name" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."tool_name" IS '@access: confidential';
-- Set comment to column: "failure_category" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."failure_category" IS '@access: confidential';
-- Set comment to column: "note" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."note" IS '@access: opaque-restricted';
-- Set comment to column: "delivery_state" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."delivery_state" IS '@access: confidential';
-- Set comment to column: "delivery_attempts" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."delivery_attempts" IS '@access: confidential';
-- Set comment to column: "last_delivery_attempt_at" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."last_delivery_attempt_at" IS '@access: confidential';
-- Set comment to column: "delivered_at" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."delivered_at" IS '@access: confidential';
-- Set comment to column: "dead_lettered_at" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."dead_lettered_at" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_feedback"
COMMENT ON COLUMN "platform_mcp_feedback"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."id" IS '@access: confidential';
-- Set comment to column: "client_id" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_id" IS '@access: restricted';
-- Set comment to column: "client_secret_hash" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_secret_hash" IS '@access: secret-restricted';
-- Set comment to column: "client_name" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_name" IS '@access: opaque-restricted';
-- Set comment to column: "redirect_uris" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."redirect_uris" IS '@access: opaque-restricted';
-- Set comment to column: "client_id_issued_at" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_id_issued_at" IS '@access: confidential';
-- Set comment to column: "client_secret_expires_at" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_uri" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_id_metadata_uri" IS '@access: restricted';
-- Set comment to column: "client_id_metadata_fetched_at" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_id_metadata_fetched_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_cache_expires_at" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_id_metadata_cache_expires_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_etag" on table: "platform_mcp_oauth_clients"
COMMENT ON COLUMN "platform_mcp_oauth_clients"."client_id_metadata_etag" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."organization_id" IS '@access: confidential';
-- Set comment to column: "milestone" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."milestone" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."connection_generation" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."project_id" IS '@access: confidential';
-- Set comment to column: "mcp_key" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."mcp_key" IS '@access: restricted';
-- Set comment to column: "attempt_id" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."attempt_id" IS '@access: confidential';
-- Set comment to column: "product_day" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."product_day" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."created_at" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."user_id" IS '@access: confidential';
-- Set comment to column: "acting_surface" on table: "platform_mcp_onboarding_milestones"
COMMENT ON COLUMN "platform_mcp_onboarding_milestones"."acting_surface" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."organization_id" IS '@access: confidential';
-- Set comment to column: "initiating_subject_urn" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."initiating_subject_urn" IS '@access: confidential';
-- Set comment to column: "source_surface" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."source_surface" IS '@access: confidential';
-- Set comment to column: "client_family" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."client_family" IS '@access: confidential';
-- Set comment to column: "agent_configuration_copied_at" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."agent_configuration_copied_at" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."connection_generation" IS '@access: confidential';
-- Set comment to column: "selected_project_id" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."selected_project_id" IS '@access: confidential';
-- Set comment to column: "selected_registration_id" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."selected_registration_id" IS '@access: confidential';
-- Set comment to column: "status" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."status" IS '@access: confidential';
-- Set comment to column: "correlation_id" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."correlation_id" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."expires_at" IS '@access: confidential';
-- Set comment to column: "closed_at" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."closed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_onboarding_workflows"
COMMENT ON COLUMN "platform_mcp_onboarding_workflows"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."project_id" IS '@access: confidential';
-- Set comment to column: "registration_id" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."registration_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."connection_generation" IS '@access: confidential';
-- Set comment to column: "operation" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."operation" IS '@access: confidential';
-- Set comment to column: "idempotency_key" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."idempotency_key" IS '@access: opaque-restricted';
-- Set comment to column: "input_hash" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."input_hash" IS '@access: confidential';
-- Set comment to column: "status" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."status" IS '@access: confidential';
-- Set comment to column: "result_code" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."result_code" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."updated_at" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."user_id" IS '@access: confidential';
-- Set comment to column: "acting_surface" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."acting_surface" IS '@access: confidential';
-- Set comment to column: "result_payload" on table: "platform_mcp_operation_receipts"
COMMENT ON COLUMN "platform_mcp_operation_receipts"."result_payload" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."project_id" IS '@access: confidential';
-- Set comment to column: "registration_id" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."registration_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."connection_generation" IS '@access: confidential';
-- Set comment to column: "provider_authorization_fingerprint" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."provider_authorization_fingerprint" IS '@access: confidential';
-- Set comment to column: "state" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."state" IS '@access: confidential';
-- Set comment to column: "evidence_code" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."evidence_code" IS '@access: confidential';
-- Set comment to column: "checked_at" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."checked_at" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."updated_at" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."user_id" IS '@access: confidential';
-- Set comment to column: "acting_surface" on table: "platform_mcp_readiness"
COMMENT ON COLUMN "platform_mcp_readiness"."acting_surface" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."project_id" IS '@access: confidential';
-- Set comment to column: "registration_id" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."registration_id" IS '@access: confidential';
-- Set comment to column: "distribution_id" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."distribution_id" IS '@access: confidential';
-- Set comment to column: "distribution_version" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."distribution_version" IS '@access: confidential';
-- Set comment to column: "workflow_id" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."workflow_id" IS '@access: confidential';
-- Set comment to column: "tool_name" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."tool_name" IS '@access: confidential';
-- Set comment to column: "tool_category" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."tool_category" IS '@access: confidential';
-- Set comment to column: "request_reference" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."request_reference" IS '@access: restricted';
-- Set comment to column: "succeeded_at" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."succeeded_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_selected_use_evidence"
COMMENT ON COLUMN "platform_mcp_selected_use_evidence"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."organization_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."connection_id" IS '@access: confidential';
-- Set comment to column: "oauth_client_id" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."connection_generation" IS '@access: confidential';
-- Set comment to column: "jti" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."jti" IS '@access: secret-restricted';
-- Set comment to column: "refresh_token_hash" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."refresh_token_hash" IS '@access: secret-restricted';
-- Set comment to column: "expires_at" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."expires_at" IS '@access: confidential';
-- Set comment to column: "refresh_expires_at" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."refresh_expires_at" IS '@access: confidential';
-- Set comment to column: "rotated_at" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."rotated_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."revoked_at" IS '@access: confidential';
-- Set comment to column: "replaced_by_session_id" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."replaced_by_session_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_sessions"
COMMENT ON COLUMN "platform_mcp_sessions"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."project_id" IS '@access: confidential';
-- Set comment to column: "registration_id" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."registration_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."connection_generation" IS '@access: confidential';
-- Set comment to column: "provider_key" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."provider_key" IS '@access: confidential';
-- Set comment to column: "intent" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."intent" IS '@access: confidential';
-- Set comment to column: "handoff_hash" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."handoff_hash" IS '@access: secret-restricted';
-- Set comment to column: "expires_at" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."expires_at" IS '@access: confidential';
-- Set comment to column: "redeemed_at" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."redeemed_at" IS '@access: confidential';
-- Set comment to column: "invalidated_at" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."invalidated_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."updated_at" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."user_id" IS '@access: confidential';
-- Set comment to column: "acting_surface" on table: "platform_mcp_setup_handoffs"
COMMENT ON COLUMN "platform_mcp_setup_handoffs"."acting_surface" IS '@access: confidential';
-- Set comment to column: "id" on table: "plugin_assignments"
COMMENT ON COLUMN "plugin_assignments"."id" IS '@access: confidential';
-- Set comment to column: "plugin_id" on table: "plugin_assignments"
COMMENT ON COLUMN "plugin_assignments"."plugin_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "plugin_assignments"
COMMENT ON COLUMN "plugin_assignments"."organization_id" IS '@access: confidential';
-- Set comment to column: "principal_urn" on table: "plugin_assignments"
COMMENT ON COLUMN "plugin_assignments"."principal_urn" IS '@access: confidential-pii';
-- Set comment to column: "created_at" on table: "plugin_assignments"
COMMENT ON COLUMN "plugin_assignments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "plugin_assignments"
COMMENT ON COLUMN "plugin_assignments"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."project_id" IS '@access: confidential';
-- Set comment to column: "installation_id" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."installation_id" IS '@access: confidential';
-- Set comment to column: "repo_owner" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."repo_owner" IS '@access: confidential-pii';
-- Set comment to column: "repo_name" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."repo_name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."updated_at" IS '@access: confidential';
-- Set comment to column: "marketplace_token" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."marketplace_token" IS '@access: secret-restricted';
-- Set comment to column: "published_fingerprint" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."published_fingerprint" IS '@access: confidential';
-- Set comment to column: "published_mcp_fingerprints" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."published_mcp_fingerprints" IS '@access: confidential';
-- Set comment to column: "published_hooks_version" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."published_hooks_version" IS '@access: confidential';
-- Set comment to column: "published_hooks_config" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."published_hooks_config" IS '@access: confidential';
-- Set comment to column: "id" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."id" IS '@access: confidential';
-- Set comment to column: "plugin_id" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."plugin_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."toolset_id" IS '@access: confidential';
-- Set comment to column: "display_name" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."display_name" IS '@access: confidential';
-- Set comment to column: "policy" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."policy" IS '@access: confidential';
-- Set comment to column: "sort_order" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."sort_order" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."deleted" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "meta_mcp_server_id" on table: "plugin_servers"
COMMENT ON COLUMN "plugin_servers"."meta_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "plugins"
COMMENT ON COLUMN "plugins"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "plugins"
COMMENT ON COLUMN "plugins"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "plugins"
COMMENT ON COLUMN "plugins"."project_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "plugins"
COMMENT ON COLUMN "plugins"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "plugins"
COMMENT ON COLUMN "plugins"."slug" IS '@access: confidential';
-- Set comment to column: "description" on table: "plugins"
COMMENT ON COLUMN "plugins"."description" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "plugins"
COMMENT ON COLUMN "plugins"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "plugins"
COMMENT ON COLUMN "plugins"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "plugins"
COMMENT ON COLUMN "plugins"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "plugins"
COMMENT ON COLUMN "plugins"."deleted" IS '@access: confidential';
-- Set comment to column: "is_default" on table: "plugins"
COMMENT ON COLUMN "plugins"."is_default" IS 'Marks the fallback plugin new servers land in when not explicitly routed to a named plugin. At most one true per project (see plugins_project_id_is_default_key).
@access: confidential';
-- Set comment to column: "id" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."organization_id" IS 'The organization this grant belongs to. Grants are always org-scoped.
@access: confidential';
-- Set comment to column: "principal_urn" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."principal_urn" IS 'URN identifying the principal, e.g. "user:user_abc", "role:admin". Format is type:id.
@access: confidential-pii';
-- Set comment to column: "principal_type" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."principal_type" IS 'Derived from principal_urn. The type prefix, e.g. "user", "role".
@access: confidential';
-- Set comment to column: "scope" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."scope" IS 'The scope being granted, e.g. "build:read". Validated in application code, not via FK.
@access: confidential';
-- Set comment to column: "drop_resource" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."drop_resource" IS 'Deprecated. Formerly ''*'' = unrestricted. Nullable, scheduled for removal.
@access: confidential';
-- Set comment to column: "created_at" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."updated_at" IS '@access: confidential';
-- Set comment to column: "selectors" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."selectors" IS 'JSON selector constraints attached to a grant. Must be a non-empty JSONB object. Wildcard/unrestricted grants use {"resource_kind":"*","resource_id":"*"}.
@access: opaque-restricted';
-- Set comment to column: "effect" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."effect" IS 'Whether this grant allows or denies the scope. NULL = allow for backward compatibility.
@access: confidential';
-- Set comment to column: "id" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."organization_id" IS '@access: confidential';
-- Set comment to column: "principal_id" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."principal_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "remote_session_client_id" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "remote_session_id" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."remote_session_id" IS '@access: confidential';
-- Set comment to column: "issuer_attachment_scope" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."issuer_attachment_scope" IS '@access: confidential';
-- Set comment to column: "client_attachment_scope" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."client_attachment_scope" IS '@access: confidential';
-- Set comment to column: "grant_generation" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."grant_generation" IS '@access: confidential';
-- Set comment to column: "attached_by_subject_id" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."attached_by_subject_id" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "principal_remote_session_bindings"
COMMENT ON COLUMN "principal_remote_session_bindings"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "project_allowed_origins"
COMMENT ON COLUMN "project_allowed_origins"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "project_allowed_origins"
COMMENT ON COLUMN "project_allowed_origins"."project_id" IS '@access: confidential';
-- Set comment to column: "origin" on table: "project_allowed_origins"
COMMENT ON COLUMN "project_allowed_origins"."origin" IS '@access: restricted';
-- Set comment to column: "status" on table: "project_allowed_origins"
COMMENT ON COLUMN "project_allowed_origins"."status" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "project_allowed_origins"
COMMENT ON COLUMN "project_allowed_origins"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "project_allowed_origins"
COMMENT ON COLUMN "project_allowed_origins"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "project_allowed_origins"
COMMENT ON COLUMN "project_allowed_origins"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "project_allowed_origins"
COMMENT ON COLUMN "project_allowed_origins"."deleted" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "project_managed_assistants"
COMMENT ON COLUMN "project_managed_assistants"."project_id" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "project_managed_assistants"
COMMENT ON COLUMN "project_managed_assistants"."assistant_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "project_managed_assistants"
COMMENT ON COLUMN "project_managed_assistants"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "project_managed_assistants"
COMMENT ON COLUMN "project_managed_assistants"."updated_at" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "project_marketplace_settings"
COMMENT ON COLUMN "project_marketplace_settings"."project_id" IS '@access: confidential';
-- Set comment to column: "marketplace_name" on table: "project_marketplace_settings"
COMMENT ON COLUMN "project_marketplace_settings"."marketplace_name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "project_marketplace_settings"
COMMENT ON COLUMN "project_marketplace_settings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "project_marketplace_settings"
COMMENT ON COLUMN "project_marketplace_settings"."updated_at" IS '@access: confidential';
-- Set comment to column: "observability_enabled" on table: "project_marketplace_settings"
COMMENT ON COLUMN "project_marketplace_settings"."observability_enabled" IS '@access: confidential';
-- Set comment to column: "id" on table: "project_tool_variations"
COMMENT ON COLUMN "project_tool_variations"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "project_tool_variations"
COMMENT ON COLUMN "project_tool_variations"."project_id" IS '@access: confidential';
-- Set comment to column: "group_id" on table: "project_tool_variations"
COMMENT ON COLUMN "project_tool_variations"."group_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "projects"
COMMENT ON COLUMN "projects"."id" IS '@access: confidential';
-- Set comment to column: "name" on table: "projects"
COMMENT ON COLUMN "projects"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "projects"
COMMENT ON COLUMN "projects"."slug" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "projects"
COMMENT ON COLUMN "projects"."organization_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "projects"
COMMENT ON COLUMN "projects"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "projects"
COMMENT ON COLUMN "projects"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "projects"
COMMENT ON COLUMN "projects"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "projects"
COMMENT ON COLUMN "projects"."deleted" IS '@access: confidential';
-- Set comment to column: "logo_asset_id" on table: "projects"
COMMENT ON COLUMN "projects"."logo_asset_id" IS '@access: confidential';
-- Set comment to column: "functions_runner_version" on table: "projects"
COMMENT ON COLUMN "projects"."functions_runner_version" IS '@access: confidential';
-- Set comment to column: "id" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."project_id" IS '@access: confidential';
-- Set comment to column: "history_id" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."history_id" IS '@access: confidential';
-- Set comment to column: "predecessor_id" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."predecessor_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."description" IS '@access: opaque-restricted';
-- Set comment to column: "arguments" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."arguments" IS '@access: opaque-restricted';
-- Set comment to column: "prompt" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."prompt" IS '@access: opaque-restricted';
-- Set comment to column: "engine" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."engine" IS '@access: confidential';
-- Set comment to column: "kind" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."kind" IS '@access: confidential';
-- Set comment to column: "tools_hint" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."tools_hint" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."deleted" IS '@access: confidential';
-- Set comment to column: "tool_urn" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."tool_urn" IS '@access: confidential';
-- Set comment to column: "tool_urns_hint" on table: "prompt_templates"
COMMENT ON COLUMN "prompt_templates"."tool_urns_hint" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."id" IS '@access: confidential';
-- Set comment to column: "public_id" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."public_id" IS 'Stable id a producer can put inside its own message body. Deliberately unindexed: nothing looks a row up by it, so an index here would buy nothing and cost a uniqueness check on the caller''s transaction. Collisions are prevented by minting uuidv7, not by the database.
@access: confidential';
-- Set comment to column: "organization_id" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."organization_id" IS 'Owning organization, carried through to the published message. Deliberately not a foreign key: the check would take a KEY SHARE lock on the organization row for every enqueue, and a stream of those against one busy org generates multixacts on a row that other writers update. Rows live seconds and the relay never joins to the organization, so an org deleted mid-flight leaves rows that publish and then delete themselves. Nothing downstream may reference the organization either: publish_outbox_dead_letters drops its foreign key for the same reason, since a row that outlived its organization still has to be able to reach it.
@access: confidential';
-- Set comment to column: "topic" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."topic" IS 'Proto full name of the topic-declaring message, e.g. "gram.webhooks.v1.Event". Resolved through the outbox topic registry at publish time.
@access: confidential';
-- Set comment to column: "message" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."message" IS 'proto.Marshal of that message, published verbatim. Topic proto changes must stay additive: a row marshaled by one binary may be published after the topic schema has rolled forward.
@access: opaque-restricted';
-- Set comment to column: "attributes" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."attributes" IS 'Pub/Sub message attributes. Carries the producer traceparent so the trace survives the database hop. content-type and schema are derived at publish time and cannot be overridden from here.
@access: opaque-restricted';
-- Set comment to column: "attempts" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."attempts" IS 'Incremented when a row is claimed, not when it fails, so it counts deliveries attempted — the number dead-lettering acts on.
@access: confidential';
-- Set comment to column: "last_error" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."last_error" IS '@access: opaque-restricted';
-- Set comment to column: "retry_after" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."retry_after" IS '@access: confidential';
-- Set comment to column: "locked_until" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."locked_until" IS 'Claim lease held by the draining relay. Deliberately absent from every index predicate: predicate columns are HOT-blocking, so indexing this would force a new index tuple on every claim.
@access: confidential';
-- Set comment to column: "created_at" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."updated_at" IS '@access: confidential';
-- Set comment to column: "lease_token" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."lease_token" IS 'Identifies the claim currently holding the row, minted by the drainer. Settlement matches on it so a drain that outlived its lease cannot delete, dead-letter or release a row another drainer has since claimed. NULL means unclaimed. Unindexed, like locked_until, so claiming stays a HOT update.
@access: confidential';
-- Set comment to column: "id" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."id" IS '@access: confidential';
-- Set comment to column: "public_id" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."public_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."organization_id" IS '@access: confidential';
-- Set comment to column: "topic" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."topic" IS '@access: confidential';
-- Set comment to column: "message" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."message" IS '@access: opaque-restricted';
-- Set comment to column: "attributes" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."attributes" IS '@access: opaque-restricted';
-- Set comment to column: "attempts" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."attempts" IS '@access: confidential';
-- Set comment to column: "last_error" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."last_error" IS '@access: opaque-restricted';
-- Set comment to column: "enqueued_at" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."enqueued_at" IS 'created_at of the originating publish_outbox row, preserved so the delay before giving up stays visible after the row moves.
@access: confidential';
-- Set comment to column: "created_at" on table: "publish_outbox_dead_letters"
COMMENT ON COLUMN "publish_outbox_dead_letters"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "queries"
COMMENT ON COLUMN "queries"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "queries"
COMMENT ON COLUMN "queries"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "queries"
COMMENT ON COLUMN "queries"."organization_id" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "queries"
COMMENT ON COLUMN "queries"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "queries"
COMMENT ON COLUMN "queries"."name" IS '@access: confidential';
-- Set comment to column: "dataset" on table: "queries"
COMMENT ON COLUMN "queries"."dataset" IS '@access: confidential';
-- Set comment to column: "spec" on table: "queries"
COMMENT ON COLUMN "queries"."spec" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "queries"
COMMENT ON COLUMN "queries"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "queries"
COMMENT ON COLUMN "queries"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "queries"
COMMENT ON COLUMN "queries"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "queries"
COMMENT ON COLUMN "queries"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."id" IS '@access: confidential';
-- Set comment to column: "remote_mcp_server_id" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."remote_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."description" IS '@access: opaque-restricted';
-- Set comment to column: "is_required" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."is_required" IS '@access: confidential';
-- Set comment to column: "is_secret" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."is_secret" IS '@access: confidential';
-- Set comment to column: "value" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."value" IS '@access: secret-restricted';
-- Set comment to column: "value_from_request_header" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."value_from_request_header" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "transport_type" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."transport_type" IS '@access: confidential';
-- Set comment to column: "url" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."url" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."deleted" IS '@access: confidential';
-- Set comment to column: "name" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "remote_mcp_servers"
COMMENT ON COLUMN "remote_mcp_servers"."slug" IS '@access: restricted';
-- Set comment to column: "remote_session_client_id" on table: "remote_session_client_user_session_issuers"
COMMENT ON COLUMN "remote_session_client_user_session_issuers"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "remote_session_client_user_session_issuers"
COMMENT ON COLUMN "remote_session_client_user_session_issuers"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "remote_session_client_user_session_issuers"
COMMENT ON COLUMN "remote_session_client_user_session_issuers"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."project_id" IS '@access: confidential';
-- Set comment to column: "remote_session_issuer_id" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."remote_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "client_id" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_id" IS '@access: restricted';
-- Set comment to column: "client_secret_encrypted" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_secret_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "client_id_issued_at" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_id_issued_at" IS '@access: confidential';
-- Set comment to column: "client_secret_expires_at" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."deleted" IS '@access: confidential';
-- Set comment to column: "token_endpoint_auth_method" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."token_endpoint_auth_method" IS '@access: confidential';
-- Set comment to column: "scope" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."scope" IS '@access: opaque-restricted';
-- Set comment to column: "audience" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."audience" IS '@access: restricted';
-- Set comment to column: "legacy_callback_url" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."legacy_callback_url" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_uri" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_id_metadata_uri" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."organization_id" IS '@access: confidential';
-- Set comment to column: "json_web_key_set_id" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."json_web_key_set_id" IS '@access: confidential';
-- Set comment to column: "token_endpoint_auth_audience_format" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."token_endpoint_auth_audience_format" IS '@access: confidential';
-- Set comment to column: "resource_identifier" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."resource_identifier" IS '@access: restricted';
-- Set comment to column: "resource_name" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."resource_name" IS '@access: opaque-restricted';
-- Set comment to column: "resource_documentation" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."resource_documentation" IS '@access: restricted';
-- Set comment to column: "resource_policy_uri" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."resource_policy_uri" IS '@access: restricted';
-- Set comment to column: "resource_tos_uri" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."resource_tos_uri" IS '@access: restricted';
-- Set comment to column: "upstream_rejected_at" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."upstream_rejected_at" IS '@access: confidential';
-- Set comment to column: "attachment_scope" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."attachment_scope" IS '@access: confidential';
-- Set comment to column: "grant_types" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."grant_types" IS '@access: opaque-restricted';
-- Set comment to column: "identity_provider_connection_id" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."identity_provider_connection_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."organization_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "remote_session_issuer_id" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."remote_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "resource" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."resource" IS '@access: restricted';
-- Set comment to column: "remote_session_client_id" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "generation" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."generation" IS '@access: confidential';
-- Set comment to column: "state" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."state" IS '@access: confidential';
-- Set comment to column: "grant_source" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."grant_source" IS '@access: confidential';
-- Set comment to column: "requested_scopes" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."requested_scopes" IS '@access: opaque-restricted';
-- Set comment to column: "claim_id" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."claim_id" IS '@access: confidential';
-- Set comment to column: "claimed_at" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."claimed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "remote_session_ema_bindings"
COMMENT ON COLUMN "remote_session_ema_bindings"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."project_id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."slug" IS '@access: confidential';
-- Set comment to column: "issuer" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."issuer" IS '@access: restricted';
-- Set comment to column: "authorization_endpoint" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."authorization_endpoint" IS '@access: restricted';
-- Set comment to column: "token_endpoint" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."token_endpoint" IS '@access: restricted';
-- Set comment to column: "registration_endpoint" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."registration_endpoint" IS '@access: restricted';
-- Set comment to column: "jwks_uri" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."jwks_uri" IS '@access: restricted';
-- Set comment to column: "scopes_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."scopes_supported" IS '@access: opaque-restricted';
-- Set comment to column: "grant_types_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."grant_types_supported" IS '@access: opaque-restricted';
-- Set comment to column: "response_types_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."response_types_supported" IS '@access: opaque-restricted';
-- Set comment to column: "token_endpoint_auth_methods_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."token_endpoint_auth_methods_supported" IS '@access: opaque-restricted';
-- Set comment to column: "oidc" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."oidc" IS '@access: confidential';
-- Set comment to column: "passthrough" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."passthrough" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."deleted" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."organization_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."name" IS '@access: confidential';
-- Set comment to column: "logo_asset_id" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."logo_asset_id" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_document_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."client_id_metadata_document_supported" IS '@access: confidential';
-- Set comment to column: "service_documentation" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."service_documentation" IS '@access: restricted';
-- Set comment to column: "op_policy_uri" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."op_policy_uri" IS '@access: restricted';
-- Set comment to column: "op_tos_uri" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."op_tos_uri" IS '@access: restricted';
-- Set comment to column: "client_setup_documentation_url" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."client_setup_documentation_url" IS '@access: restricted';
-- Set comment to column: "revocation_endpoint" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."revocation_endpoint" IS '@access: restricted';
-- Set comment to column: "code_challenge_methods_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."code_challenge_methods_supported" IS '@access: opaque-restricted';
-- Set comment to column: "metadata" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."metadata" IS '@access: opaque-restricted';
-- Set comment to column: "tunneled_mcp_server_id" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."tunneled_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "userinfo_endpoint" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."userinfo_endpoint" IS '@access: restricted';
-- Set comment to column: "introspection_endpoint" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."introspection_endpoint" IS '@access: restricted';
-- Set comment to column: "introspection_endpoint_auth_methods_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."introspection_endpoint_auth_methods_supported" IS '@access: opaque-restricted';
-- Set comment to column: "id_token_signing_alg_values_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."id_token_signing_alg_values_supported" IS '@access: opaque-restricted';
-- Set comment to column: "claims_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."claims_supported" IS '@access: opaque-restricted';
-- Set comment to column: "backchannel_logout_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."backchannel_logout_supported" IS '@access: confidential';
-- Set comment to column: "authorization_response_iss_parameter_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."authorization_response_iss_parameter_supported" IS '@access: confidential';
-- Set comment to column: "metadata_fetched_at" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."metadata_fetched_at" IS '@access: confidential';
-- Set comment to column: "metadata_last_error" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."metadata_last_error" IS '@access: opaque-restricted';
-- Set comment to column: "metadata_last_error_at" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."metadata_last_error_at" IS '@access: confidential';
-- Set comment to column: "metadata_last_error_url" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."metadata_last_error_url" IS '@access: restricted';
-- Set comment to column: "scope_override" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."scope_override" IS '@access: opaque-restricted';
-- Set comment to column: "resource_indicator_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."resource_indicator_supported" IS '@access: confidential';
-- Set comment to column: "jwks" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."jwks" IS '@access: opaque-restricted';
-- Set comment to column: "jwks_fetched_at" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."jwks_fetched_at" IS '@access: confidential';
-- Set comment to column: "jwks_cache_expires_at" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."jwks_cache_expires_at" IS '@access: confidential';
-- Set comment to column: "jwks_etag" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."jwks_etag" IS '@access: opaque-restricted';
-- Set comment to column: "jwks_last_error" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."jwks_last_error" IS '@access: opaque-restricted';
-- Set comment to column: "jwks_last_error_at" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."jwks_last_error_at" IS '@access: confidential';
-- Set comment to column: "authorization_grant_profiles_supported" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."authorization_grant_profiles_supported" IS '@access: opaque-restricted';
-- Set comment to column: "attachment_scope" on table: "remote_session_issuers"
COMMENT ON COLUMN "remote_session_issuers"."attachment_scope" IS '@access: confidential';
-- Set comment to column: "id" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."id" IS '@access: confidential';
-- Set comment to column: "subject_urn" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."subject_urn" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "remote_session_client_id" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "access_token_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."access_token_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "access_expires_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."access_expires_at" IS '@access: confidential';
-- Set comment to column: "refresh_token_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."refresh_token_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "refresh_expires_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."refresh_expires_at" IS '@access: confidential';
-- Set comment to column: "scopes" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."scopes" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."deleted" IS '@access: confidential';
-- Set comment to column: "authorization_expires_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."authorization_expires_at" IS '@access: confidential';
-- Set comment to column: "resource" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."resource" IS '@access: restricted';
-- Set comment to column: "auto_refresh" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."auto_refresh" IS '@access: confidential';
-- Set comment to column: "last_refresh_attempt_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."last_refresh_attempt_at" IS '@access: confidential';
-- Set comment to column: "last_used_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."last_used_at" IS '@access: confidential';
-- Set comment to column: "upstream_subject" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."upstream_subject" IS '@access: confidential-pii';
-- Set comment to column: "upstream_email" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."upstream_email" IS '@access: confidential-pii';
-- Set comment to column: "upstream_display_name" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."upstream_display_name" IS '@access: confidential-pii';
-- Set comment to column: "identity_source" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."identity_source" IS '@access: confidential';
-- Set comment to column: "enrichment" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."enrichment" IS '@access: opaque-restricted';
-- Set comment to column: "last_validated_at" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."last_validated_at" IS '@access: confidential';
-- Set comment to column: "validation_status" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."validation_status" IS '@access: confidential';
-- Set comment to column: "validation_reason" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."validation_reason" IS '@access: opaque-restricted';
-- Set comment to column: "grant_generation" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."grant_generation" IS '@access: confidential';
-- Set comment to column: "id" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."organization_id" IS '@access: confidential';
-- Set comment to column: "rule_id" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."rule_id" IS '@access: confidential';
-- Set comment to column: "title" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."title" IS '@access: confidential';
-- Set comment to column: "description" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."description" IS '@access: opaque-restricted';
-- Set comment to column: "regex" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."regex" IS '@access: opaque-restricted';
-- Set comment to column: "severity" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."severity" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."deleted" IS '@access: confidential';
-- Set comment to column: "match_config" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."match_config" IS '@access: opaque-restricted';
-- Set comment to column: "detection_expr" on table: "risk_custom_detection_rules"
COMMENT ON COLUMN "risk_custom_detection_rules"."detection_expr" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."organization_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_id" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."risk_policy_id" IS '@access: confidential';
-- Set comment to column: "match_type" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."match_type" IS '@access: confidential';
-- Set comment to column: "match_value" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."match_value" IS '@access: secret-restricted';
-- Set comment to column: "rule_id_filter" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."rule_id_filter" IS '@access: confidential';
-- Set comment to column: "source_filter" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."source_filter" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."enabled" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "risk_exclusions"
COMMENT ON COLUMN "risk_exclusions"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."organization_id" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."enabled" IS '@access: confidential';
-- Set comment to column: "name" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."name" IS '@access: confidential';
-- Set comment to column: "sources" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."sources" IS '@access: confidential';
-- Set comment to column: "version" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."version" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."deleted" IS '@access: confidential';
-- Set comment to column: "presidio_entities" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."presidio_entities" IS '@access: opaque-restricted';
-- Set comment to column: "action" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."action" IS '@access: confidential';
-- Set comment to column: "auto_name" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."auto_name" IS '@access: confidential';
-- Set comment to column: "user_message" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."user_message" IS '@access: opaque-restricted';
-- Set comment to column: "prompt_injection_rules" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."prompt_injection_rules" IS '@access: opaque-restricted';
-- Set comment to column: "disabled_rules" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."disabled_rules" IS '@access: opaque-restricted';
-- Set comment to column: "custom_rule_ids" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."custom_rule_ids" IS '@access: confidential';
-- Set comment to column: "audience_type" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."audience_type" IS '@access: confidential';
-- Set comment to column: "policy_type" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."policy_type" IS '@access: confidential';
-- Set comment to column: "prompt" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."prompt" IS '@access: opaque-restricted';
-- Set comment to column: "model_config" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."model_config" IS '@access: confidential';
-- Set comment to column: "analyzer_config" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."analyzer_config" IS '@access: opaque-restricted';
-- Set comment to column: "score" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."score" IS '@access: confidential';
-- Set comment to column: "shadow_mcp_disposition" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."shadow_mcp_disposition" IS '@access: confidential';
-- Set comment to column: "mcp_scope" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."mcp_scope" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."project_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_id" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."risk_policy_id" IS '@access: confidential';
-- Set comment to column: "target_kind" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."target_kind" IS 'Generic target namespace for the bypass request, such as server_url. Empty means the whole policy.
@access: confidential';
-- Set comment to column: "target_label" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."target_label" IS '@access: restricted';
-- Set comment to column: "target_key" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."target_key" IS 'Stable canonical key for deduplicating bypass requests within the target namespace.
@access: opaque-restricted';
-- Set comment to column: "target_dimensions" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."target_dimensions" IS 'Selector dimensions for the target, such as {"server_url":"mcp.example.com"}.
@access: opaque-restricted';
-- Set comment to column: "requester_user_id" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."requester_user_id" IS '@access: confidential';
-- Set comment to column: "requester_email" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."requester_email" IS '@access: confidential-pii';
-- Set comment to column: "note" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."note" IS '@access: opaque-restricted';
-- Set comment to column: "status" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."status" IS '@access: confidential';
-- Set comment to column: "decided_by" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."decided_by" IS '@access: confidential';
-- Set comment to column: "granted_principal_urns" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."granted_principal_urns" IS '@access: confidential';
-- Set comment to column: "decided_at" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."decided_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "risk_policy_bypass_requests"
COMMENT ON COLUMN "risk_policy_bypass_requests"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."project_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_id" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."risk_policy_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."user_id" IS '@access: confidential';
-- Set comment to column: "tool_name" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."tool_name" IS '@access: confidential';
-- Set comment to column: "status" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."status" IS '@access: confidential';
-- Set comment to column: "policy_name" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."policy_name" IS '@access: confidential';
-- Set comment to column: "entity" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."entity" IS '@access: confidential';
-- Set comment to column: "rule_id" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."rule_id" IS '@access: confidential';
-- Set comment to column: "challenged_at" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."challenged_at" IS '@access: confidential';
-- Set comment to column: "acknowledged_at" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."acknowledged_at" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."deleted" IS '@access: confidential';
-- Set comment to column: "call_fingerprint" on table: "risk_policy_challenges"
COMMENT ON COLUMN "risk_policy_challenges"."call_fingerprint" IS '@access: confidential';
-- Set comment to column: "id" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."organization_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_id" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."risk_policy_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_version" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."risk_policy_version" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."chat_id" IS '@access: confidential';
-- Set comment to column: "verdict" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."verdict" IS '@access: confidential';
-- Set comment to column: "reviewed_by" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."reviewed_by" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "risk_policy_eval_reviews"
COMMENT ON COLUMN "risk_policy_eval_reviews"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."organization_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."risk_policy_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_version" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."risk_policy_version" IS '@access: confidential';
-- Set comment to column: "chat_message_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."chat_message_id" IS '@access: confidential';
-- Set comment to column: "source" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."source" IS '@access: confidential';
-- Set comment to column: "found" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."found" IS '@access: confidential';
-- Set comment to column: "rule_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."rule_id" IS '@access: confidential';
-- Set comment to column: "description" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."description" IS '@access: opaque-restricted';
-- Set comment to column: "match" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."match" IS '@access: secret-restricted';
-- Set comment to column: "start_pos" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."start_pos" IS '@access: confidential';
-- Set comment to column: "end_pos" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."end_pos" IS '@access: confidential';
-- Set comment to column: "confidence" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."confidence" IS '@access: confidential';
-- Set comment to column: "tags" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."tags" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."created_at" IS '@access: confidential';
-- Set comment to column: "dead_letter_reason" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."dead_letter_reason" IS '@access: opaque-restricted';
-- Set comment to column: "excluded_at" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."excluded_at" IS '@access: confidential';
-- Set comment to column: "excluded_exclusion_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."excluded_exclusion_id" IS '@access: confidential';
-- Set comment to column: "false_positive_at" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."false_positive_at" IS '@access: confidential';
-- Set comment to column: "false_positive_reason" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."false_positive_reason" IS '@access: opaque-restricted';
-- Set comment to column: "spans" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."spans" IS '@access: secret-restricted';
-- Set comment to column: "chat_content_part_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."chat_content_part_id" IS '@access: confidential';
-- Set comment to column: "skill_version_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."skill_version_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."project_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."organization_id" IS '@access: confidential';
-- Set comment to column: "session_id" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."session_id" IS '@access: confidential';
-- Set comment to column: "token" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."token" IS '@access: secret-restricted';
-- Set comment to column: "blob_url" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."blob_url" IS '@access: secret-restricted';
-- Set comment to column: "created_by_email" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."created_by_email" IS '@access: confidential-pii';
-- Set comment to column: "expires_at" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."expires_at" IS '@access: confidential';
-- Set comment to column: "consumed_at" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."consumed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "session_handoff_links"
COMMENT ON COLUMN "session_handoff_links"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."project_id" IS '@access: confidential';
-- Set comment to column: "session_id" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."session_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_id" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."risk_policy_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_name" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."risk_policy_name" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."user_id" IS '@access: confidential';
-- Set comment to column: "reason" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."reason" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."updated_at" IS '@access: confidential';
-- Set comment to column: "released_at" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."released_at" IS '@access: confidential';
-- Set comment to column: "released_by" on table: "session_quarantines"
COMMENT ON COLUMN "session_quarantines"."released_by" IS '@access: confidential';
-- Set comment to column: "id" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."project_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."skill_id" IS '@access: confidential';
-- Set comment to column: "pinned_version_id" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."pinned_version_id" IS '@access: confidential';
-- Set comment to column: "plugin_id" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."plugin_id" IS '@access: confidential';
-- Set comment to column: "channel" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."channel" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."updated_at" IS '@access: confidential';
-- Set comment to column: "assistant_id" on table: "skill_distributions"
COMMENT ON COLUMN "skill_distributions"."assistant_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."project_id" IS '@access: confidential';
-- Set comment to column: "suggestion_id" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."suggestion_id" IS '@access: confidential';
-- Set comment to column: "proposed_diff" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."proposed_diff" IS '@access: opaque-restricted';
-- Set comment to column: "rationale" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."rationale" IS '@access: opaque-restricted';
-- Set comment to column: "position" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."position" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."updated_at" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_edit_suggestion_feedback"
COMMENT ON COLUMN "skill_edit_suggestion_feedback"."project_id" IS '@access: confidential';
-- Set comment to column: "change_id" on table: "skill_edit_suggestion_feedback"
COMMENT ON COLUMN "skill_edit_suggestion_feedback"."change_id" IS '@access: confidential';
-- Set comment to column: "feedback_id" on table: "skill_edit_suggestion_feedback"
COMMENT ON COLUMN "skill_edit_suggestion_feedback"."feedback_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_edit_suggestion_feedback"
COMMENT ON COLUMN "skill_edit_suggestion_feedback"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."project_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."skill_id" IS '@access: confidential';
-- Set comment to column: "base_version_id" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."base_version_id" IS '@access: confidential';
-- Set comment to column: "rationale" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."rationale" IS '@access: opaque-restricted';
-- Set comment to column: "status" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."status" IS '@access: confidential';
-- Set comment to column: "scored_session_count" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."scored_session_count" IS '@access: confidential';
-- Set comment to column: "approved_by_user_id" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."approved_by_user_id" IS '@access: confidential';
-- Set comment to column: "approved_at" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."approved_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "skill_edit_suggestions"
COMMENT ON COLUMN "skill_edit_suggestions"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."project_id" IS '@access: confidential';
-- Set comment to column: "surface" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."surface" IS '@access: confidential';
-- Set comment to column: "session_id" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."session_id" IS '@access: opaque-restricted';
-- Set comment to column: "chat_id" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."chat_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."skill_id" IS '@access: confidential';
-- Set comment to column: "skill_version_id" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."skill_version_id" IS '@access: confidential';
-- Set comment to column: "canonical_sha256" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."canonical_sha256" IS '@access: confidential';
-- Set comment to column: "observed_at" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."observed_at" IS '@access: confidential';
-- Set comment to column: "state" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."state" IS '@access: confidential';
-- Set comment to column: "reserved_on" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."reserved_on" IS '@access: confidential';
-- Set comment to column: "attempts" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."attempts" IS '@access: confidential';
-- Set comment to column: "last_error" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."last_error" IS '@access: opaque-restricted';
-- Set comment to column: "scored_at" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."scored_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."updated_at" IS '@access: confidential';
-- Set comment to column: "claim_token" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."claim_token" IS '@access: confidential';
-- Set comment to column: "failed_at" on table: "skill_efficacy_evaluations"
COMMENT ON COLUMN "skill_efficacy_evaluations"."failed_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "skill_efficacy_settings"
COMMENT ON COLUMN "skill_efficacy_settings"."organization_id" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "skill_efficacy_settings"
COMMENT ON COLUMN "skill_efficacy_settings"."enabled" IS '@access: confidential';
-- Set comment to column: "per_skill_daily_cap" on table: "skill_efficacy_settings"
COMMENT ON COLUMN "skill_efficacy_settings"."per_skill_daily_cap" IS '@access: confidential';
-- Set comment to column: "org_daily_cap" on table: "skill_efficacy_settings"
COMMENT ON COLUMN "skill_efficacy_settings"."org_daily_cap" IS '@access: confidential';
-- Set comment to column: "new_version_burst" on table: "skill_efficacy_settings"
COMMENT ON COLUMN "skill_efficacy_settings"."new_version_burst" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_efficacy_settings"
COMMENT ON COLUMN "skill_efficacy_settings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "skill_efficacy_settings"
COMMENT ON COLUMN "skill_efficacy_settings"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."project_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."skill_id" IS '@access: confidential';
-- Set comment to column: "skill_version_id" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."skill_version_id" IS '@access: confidential';
-- Set comment to column: "skill_name" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."skill_name" IS '@access: confidential';
-- Set comment to column: "source" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."source" IS '@access: confidential';
-- Set comment to column: "outcome" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."outcome" IS '@access: confidential';
-- Set comment to column: "note" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."note" IS '@access: opaque-restricted';
-- Set comment to column: "session_id" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."session_id" IS '@access: opaque-restricted';
-- Set comment to column: "user_id" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."user_id" IS '@access: confidential';
-- Set comment to column: "user_email" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."user_email" IS '@access: confidential-pii';
-- Set comment to column: "reviewed_at" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."reviewed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_feedback"
COMMENT ON COLUMN "skill_feedback"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."project_id" IS '@access: confidential';
-- Set comment to column: "idempotency_key" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."idempotency_key" IS '@access: opaque-restricted';
-- Set comment to column: "provider" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."provider" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."user_id" IS '@access: confidential';
-- Set comment to column: "user_email" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."user_email" IS '@access: confidential-pii';
-- Set comment to column: "hostname" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."hostname" IS '@access: confidential-pii';
-- Set comment to column: "session_id" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."session_id" IS '@access: opaque-restricted';
-- Set comment to column: "skill_name" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."skill_name" IS '@access: confidential';
-- Set comment to column: "source_level" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."source_level" IS '@access: opaque-restricted';
-- Set comment to column: "source_path" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."source_path" IS '@access: opaque-restricted';
-- Set comment to column: "raw_sha256" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."raw_sha256" IS '@access: confidential';
-- Set comment to column: "seen_at" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."seen_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."created_at" IS '@access: confidential';
-- Set comment to column: "source" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."source" IS '@access: opaque-restricted';
-- Set comment to column: "skill_id" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."skill_id" IS '@access: confidential';
-- Set comment to column: "reconciled_at" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."reconciled_at" IS '@access: confidential';
-- Set comment to column: "reconcile_error_code" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."reconcile_error_code" IS '@access: confidential';
-- Set comment to column: "skill_version_id" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."skill_version_id" IS '@access: confidential';
-- Set comment to column: "metrics_synced_at" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."metrics_synced_at" IS '@access: confidential';
-- Set comment to column: "efficacy_enqueued_at" on table: "skill_observations"
COMMENT ON COLUMN "skill_observations"."efficacy_enqueued_at" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_raw_hashes"
COMMENT ON COLUMN "skill_raw_hashes"."project_id" IS '@access: confidential';
-- Set comment to column: "raw_sha256" on table: "skill_raw_hashes"
COMMENT ON COLUMN "skill_raw_hashes"."raw_sha256" IS '@access: confidential';
-- Set comment to column: "canonical_sha256" on table: "skill_raw_hashes"
COMMENT ON COLUMN "skill_raw_hashes"."canonical_sha256" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_raw_hashes"
COMMENT ON COLUMN "skill_raw_hashes"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "skill_share_links"
COMMENT ON COLUMN "skill_share_links"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_share_links"
COMMENT ON COLUMN "skill_share_links"."project_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_share_links"
COMMENT ON COLUMN "skill_share_links"."skill_id" IS '@access: confidential';
-- Set comment to column: "token" on table: "skill_share_links"
COMMENT ON COLUMN "skill_share_links"."token" IS '@access: secret-restricted';
-- Set comment to column: "created_by_user_id" on table: "skill_share_links"
COMMENT ON COLUMN "skill_share_links"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "skill_share_links"
COMMENT ON COLUMN "skill_share_links"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_share_links"
COMMENT ON COLUMN "skill_share_links"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "skill_share_links"
COMMENT ON COLUMN "skill_share_links"."updated_at" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."project_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."skill_id" IS '@access: confidential';
-- Set comment to column: "skill_version_id" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."skill_version_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."user_id" IS '@access: confidential';
-- Set comment to column: "hostname" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."hostname" IS '@access: confidential-pii';
-- Set comment to column: "provider" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."provider" IS '@access: confidential';
-- Set comment to column: "status" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."status" IS '@access: restricted';
-- Set comment to column: "synced_at" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."synced_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "skill_sync_receipts"
COMMENT ON COLUMN "skill_sync_receipts"."updated_at" IS '@access: confidential';
-- Set comment to column: "skill_version_id" on table: "skill_version_lineages"
COMMENT ON COLUMN "skill_version_lineages"."skill_version_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_version_lineages"
COMMENT ON COLUMN "skill_version_lineages"."skill_id" IS '@access: confidential';
-- Set comment to column: "derived_from_version_id" on table: "skill_version_lineages"
COMMENT ON COLUMN "skill_version_lineages"."derived_from_version_id" IS '@access: confidential';
-- Set comment to column: "skill_version_id" on table: "skill_version_origins"
COMMENT ON COLUMN "skill_version_origins"."skill_version_id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_version_origins"
COMMENT ON COLUMN "skill_version_origins"."skill_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skill_version_origins"
COMMENT ON COLUMN "skill_version_origins"."project_id" IS '@access: confidential';
-- Set comment to column: "origin" on table: "skill_version_origins"
COMMENT ON COLUMN "skill_version_origins"."origin" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skill_version_origins"
COMMENT ON COLUMN "skill_version_origins"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."id" IS '@access: confidential';
-- Set comment to column: "skill_id" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."skill_id" IS '@access: confidential';
-- Set comment to column: "content" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."content" IS '@access: opaque-restricted';
-- Set comment to column: "canonical_sha256" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."canonical_sha256" IS '@access: confidential';
-- Set comment to column: "raw_sha256" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."raw_sha256" IS '@access: confidential';
-- Set comment to column: "description" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."description" IS '@access: opaque-restricted';
-- Set comment to column: "metadata" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."metadata" IS '@access: opaque-restricted';
-- Set comment to column: "spec_valid" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."spec_valid" IS '@access: confidential';
-- Set comment to column: "validation_errors" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."validation_errors" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."created_at" IS '@access: confidential';
-- Set comment to column: "created_by_user_id" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "promoted_at" on table: "skill_versions"
COMMENT ON COLUMN "skill_versions"."promoted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "skills"
COMMENT ON COLUMN "skills"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "skills"
COMMENT ON COLUMN "skills"."project_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "skills"
COMMENT ON COLUMN "skills"."name" IS '@access: confidential';
-- Set comment to column: "display_name" on table: "skills"
COMMENT ON COLUMN "skills"."display_name" IS '@access: confidential';
-- Set comment to column: "summary" on table: "skills"
COMMENT ON COLUMN "skills"."summary" IS '@access: opaque-restricted';
-- Set comment to column: "source_kind" on table: "skills"
COMMENT ON COLUMN "skills"."source_kind" IS '@access: confidential';
-- Set comment to column: "classification" on table: "skills"
COMMENT ON COLUMN "skills"."classification" IS '@access: confidential';
-- Set comment to column: "archived_at" on table: "skills"
COMMENT ON COLUMN "skills"."archived_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "skills"
COMMENT ON COLUMN "skills"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "skills"
COMMENT ON COLUMN "skills"."updated_at" IS '@access: confidential';
-- Set comment to column: "first_seen_at" on table: "skills"
COMMENT ON COLUMN "skills"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "skills"
COMMENT ON COLUMN "skills"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "seen_count" on table: "skills"
COMMENT ON COLUMN "skills"."seen_count" IS '@access: confidential';
-- Set comment to column: "tags" on table: "skills"
COMMENT ON COLUMN "skills"."tags" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "slack_app_toolsets"
COMMENT ON COLUMN "slack_app_toolsets"."id" IS '@access: confidential';
-- Set comment to column: "slack_app_id" on table: "slack_app_toolsets"
COMMENT ON COLUMN "slack_app_toolsets"."slack_app_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "slack_app_toolsets"
COMMENT ON COLUMN "slack_app_toolsets"."toolset_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "slack_app_toolsets"
COMMENT ON COLUMN "slack_app_toolsets"."created_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."created_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."deleted_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."updated_at" IS '@access: confidential';
-- Set comment to column: "slack_team_name" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_team_name" IS '@access: confidential';
-- Set comment to column: "slack_bot_user_id" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_bot_user_id" IS '@access: confidential';
-- Set comment to column: "slack_client_secret" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_client_secret" IS '@access: secret-restricted';
-- Set comment to column: "slack_signing_secret" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_signing_secret" IS '@access: secret-restricted';
-- Set comment to column: "slack_team_id" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_team_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."organization_id" IS '@access: confidential';
-- Set comment to column: "slack_bot_token" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_bot_token" IS '@access: secret-restricted';
-- Set comment to column: "slack_client_id" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."slack_client_id" IS '@access: restricted';
-- Set comment to column: "system_prompt" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."system_prompt" IS '@access: opaque-restricted';
-- Set comment to column: "name" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."name" IS '@access: confidential';
-- Set comment to column: "status" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."status" IS '@access: confidential';
-- Set comment to column: "icon_asset_id" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."icon_asset_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."project_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."id" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "slack_apps"
COMMENT ON COLUMN "slack_apps"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."organization_id" IS '@access: confidential';
-- Set comment to column: "slack_team_id" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."slack_team_id" IS '@access: confidential';
-- Set comment to column: "slack_team_name" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."slack_team_name" IS '@access: confidential';
-- Set comment to column: "credentials_encrypted" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."credentials_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "granted_scopes" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."granted_scopes" IS '@access: opaque-restricted';
-- Set comment to column: "generation" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."generation" IS '@access: confidential';
-- Set comment to column: "health" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."health" IS '@access: confidential';
-- Set comment to column: "disconnected_at" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."disconnected_at" IS '@access: confidential';
-- Set comment to column: "last_sync_started_at" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."last_sync_started_at" IS '@access: confidential';
-- Set comment to column: "last_full_sync_generation" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."last_full_sync_generation" IS '@access: confidential';
-- Set comment to column: "last_full_sync_succeeded_at" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."last_full_sync_succeeded_at" IS '@access: confidential';
-- Set comment to column: "last_sync_failed_at" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."last_sync_failed_at" IS '@access: confidential';
-- Set comment to column: "last_error_code" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."last_error_code" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."organization_id" IS '@access: confidential';
-- Set comment to column: "slack_team_id" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."slack_team_id" IS '@access: confidential';
-- Set comment to column: "slack_user_id" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."slack_user_id" IS '@access: confidential-pii';
-- Set comment to column: "display_name" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."display_name" IS '@access: confidential-pii';
-- Set comment to column: "email" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."email" IS '@access: confidential-pii';
-- Set comment to column: "status" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."status" IS '@access: confidential';
-- Set comment to column: "member_type" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."member_type" IS '@access: confidential';
-- Set comment to column: "provider_updated_at" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."provider_updated_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "mapping_revision" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."mapping_revision" IS '@access: confidential';
-- Set comment to column: "mapping_conflict_reason" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."mapping_conflict_reason" IS '@access: confidential';
-- Set comment to column: "mapping_conflict_detected_at" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."mapping_conflict_detected_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "slack_directory_memberships"
COMMENT ON COLUMN "slack_directory_memberships"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "slack_identity_mappings"
COMMENT ON COLUMN "slack_identity_mappings"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "slack_identity_mappings"
COMMENT ON COLUMN "slack_identity_mappings"."organization_id" IS '@access: confidential';
-- Set comment to column: "slack_team_id" on table: "slack_identity_mappings"
COMMENT ON COLUMN "slack_identity_mappings"."slack_team_id" IS '@access: confidential';
-- Set comment to column: "slack_user_id" on table: "slack_identity_mappings"
COMMENT ON COLUMN "slack_identity_mappings"."slack_user_id" IS '@access: confidential-pii';
-- Set comment to column: "user_id" on table: "slack_identity_mappings"
COMMENT ON COLUMN "slack_identity_mappings"."user_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "slack_identity_mappings"
COMMENT ON COLUMN "slack_identity_mappings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "slack_identity_mappings"
COMMENT ON COLUMN "slack_identity_mappings"."updated_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "slack_identity_mappings"
COMMENT ON COLUMN "slack_identity_mappings"."revoked_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "slack_registrations"
COMMENT ON COLUMN "slack_registrations"."id" IS '@access: confidential';
-- Set comment to column: "slack_app_id" on table: "slack_registrations"
COMMENT ON COLUMN "slack_registrations"."slack_app_id" IS '@access: confidential';
-- Set comment to column: "slack_account_id" on table: "slack_registrations"
COMMENT ON COLUMN "slack_registrations"."slack_account_id" IS '@access: confidential-pii';
-- Set comment to column: "user_id" on table: "slack_registrations"
COMMENT ON COLUMN "slack_registrations"."user_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "slack_registrations"
COMMENT ON COLUMN "slack_registrations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "slack_registrations"
COMMENT ON COLUMN "slack_registrations"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "source_environments"
COMMENT ON COLUMN "source_environments"."id" IS '@access: confidential';
-- Set comment to column: "source_kind" on table: "source_environments"
COMMENT ON COLUMN "source_environments"."source_kind" IS '@access: confidential';
-- Set comment to column: "source_slug" on table: "source_environments"
COMMENT ON COLUMN "source_environments"."source_slug" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "source_environments"
COMMENT ON COLUMN "source_environments"."project_id" IS '@access: confidential';
-- Set comment to column: "environment_id" on table: "source_environments"
COMMENT ON COLUMN "source_environments"."environment_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "source_environments"
COMMENT ON COLUMN "source_environments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "source_environments"
COMMENT ON COLUMN "source_environments"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."organization_id" IS '@access: confidential';
-- Set comment to column: "spend_rule_id" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."spend_rule_id" IS '@access: confidential';
-- Set comment to column: "rule_urn" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."rule_urn" IS '@access: confidential';
-- Set comment to column: "event_type" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."event_type" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."user_id" IS '@access: confidential';
-- Set comment to column: "email" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."email" IS '@access: confidential-pii';
-- Set comment to column: "display_name" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."display_name" IS '@access: confidential-pii';
-- Set comment to column: "spend_usd_cents" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."spend_usd_cents" IS '@access: restricted';
-- Set comment to column: "limit_usd_cents" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."limit_usd_cents" IS '@access: restricted';
-- Set comment to column: "window_start" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."window_start" IS '@access: confidential';
-- Set comment to column: "window_end" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."window_end" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "spend_rule_events"
COMMENT ON COLUMN "spend_rule_events"."created_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."organization_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."slug" IS '@access: confidential';
-- Set comment to column: "description" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."description" IS '@access: opaque-restricted';
-- Set comment to column: "target_expr" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."target_expr" IS '@access: opaque-restricted';
-- Set comment to column: "limit_usd_cents" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."limit_usd_cents" IS '@access: restricted';
-- Set comment to column: "rule_expr" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."rule_expr" IS '@access: opaque-restricted';
-- Set comment to column: "window_kind" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."window_kind" IS '@access: confidential';
-- Set comment to column: "warn_at_pct" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."warn_at_pct" IS '@access: confidential';
-- Set comment to column: "action" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."action" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."enabled" IS '@access: confidential';
-- Set comment to column: "version" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."version" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."updated_at" IS '@access: confidential';
-- Set comment to column: "archived_at" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."archived_at" IS '@access: confidential';
-- Set comment to column: "archived" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."archived" IS '@access: confidential';
-- Set comment to column: "superseded_by" on table: "spend_rules"
COMMENT ON COLUMN "spend_rules"."superseded_by" IS '@access: confidential';
-- Set comment to column: "id" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."organization_id" IS '@access: confidential';
-- Set comment to column: "source_kind" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."source_kind" IS '@access: confidential';
-- Set comment to column: "source_key" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."source_key" IS '@access: confidential';
-- Set comment to column: "seq" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."seq" IS '@access: confidential';
-- Set comment to column: "source_day" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."source_day" IS '@access: confidential';
-- Set comment to column: "source_period_start" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."source_period_start" IS '@access: confidential';
-- Set comment to column: "source_period_end" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."source_period_end" IS '@access: confidential';
-- Set comment to column: "source_snapshot_usd" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."source_snapshot_usd" IS '@access: restricted';
-- Set comment to column: "delta_tokens" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."delta_tokens" IS '@access: confidential';
-- Set comment to column: "original_tum_unit_price_usd" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."original_tum_unit_price_usd" IS '@access: restricted';
-- Set comment to column: "amount_usd" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."amount_usd" IS '@access: restricted';
-- Set comment to column: "original_invoice_id" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."original_invoice_id" IS '@access: restricted';
-- Set comment to column: "destination_invoice_id" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."destination_invoice_id" IS '@access: restricted';
-- Set comment to column: "stripe_invoice_item_id" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."stripe_invoice_item_id" IS '@access: restricted';
-- Set comment to column: "stripe_credit_note_id" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."stripe_credit_note_id" IS '@access: restricted';
-- Set comment to column: "idempotency_key" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."idempotency_key" IS '@access: confidential';
-- Set comment to column: "delivery_state" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."delivery_state" IS '@access: confidential';
-- Set comment to column: "first_attempted_at" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."first_attempted_at" IS '@access: confidential';
-- Set comment to column: "last_attempted_at" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."last_attempted_at" IS '@access: confidential';
-- Set comment to column: "confirmed_at" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."confirmed_at" IS '@access: confidential';
-- Set comment to column: "ambiguous_at" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."ambiguous_at" IS '@access: confidential';
-- Set comment to column: "reconciled_at" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."reconciled_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "stripe_invoice_allocations"
COMMENT ON COLUMN "stripe_invoice_allocations"."updated_at" IS '@access: confidential';
-- Set comment to column: "stripe_invoice_id" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."stripe_invoice_id" IS '@access: restricted';
-- Set comment to column: "organization_id" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."organization_id" IS '@access: confidential';
-- Set comment to column: "stripe_customer_id" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."stripe_customer_id" IS '@access: restricted';
-- Set comment to column: "stripe_subscription_id" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."stripe_subscription_id" IS '@access: restricted';
-- Set comment to column: "service_period_start" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."service_period_start" IS '@access: confidential';
-- Set comment to column: "service_period_end" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."service_period_end" IS '@access: confidential';
-- Set comment to column: "invoice_state" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."invoice_state" IS '@access: confidential';
-- Set comment to column: "finalized_at" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."finalized_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "stripe_invoices"
COMMENT ON COLUMN "stripe_invoices"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."organization_id" IS '@access: confidential';
-- Set comment to column: "cycle_start" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."cycle_start" IS '@access: confidential';
-- Set comment to column: "seq" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."seq" IS '@access: confidential';
-- Set comment to column: "delta_tokens" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."delta_tokens" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."updated_at" IS '@access: confidential';
-- Set comment to column: "billing_cycle_usage_id" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."billing_cycle_usage_id" IS '@access: confidential';
-- Set comment to column: "cycle_end" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."cycle_end" IS '@access: confidential';
-- Set comment to column: "stripe_customer_id" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."stripe_customer_id" IS '@access: restricted';
-- Set comment to column: "stripe_meter_event_name" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."stripe_meter_event_name" IS '@access: confidential';
-- Set comment to column: "stripe_identifier" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."stripe_identifier" IS '@access: restricted';
-- Set comment to column: "event_timestamp" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."event_timestamp" IS '@access: confidential';
-- Set comment to column: "delivery_state" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."delivery_state" IS '@access: confidential';
-- Set comment to column: "first_attempted_at" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."first_attempted_at" IS '@access: confidential';
-- Set comment to column: "last_attempted_at" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."last_attempted_at" IS '@access: confidential';
-- Set comment to column: "confirmed_at" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."confirmed_at" IS '@access: confidential';
-- Set comment to column: "ambiguous_at" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."ambiguous_at" IS '@access: confidential';
-- Set comment to column: "reconciled_at" on table: "stripe_meter_reports"
COMMENT ON COLUMN "stripe_meter_reports"."reconciled_at" IS '@access: confidential';
-- Set comment to column: "stripe_event_id" on table: "stripe_webhook_receipts"
COMMENT ON COLUMN "stripe_webhook_receipts"."stripe_event_id" IS '@access: restricted';
-- Set comment to column: "organization_id" on table: "stripe_webhook_receipts"
COMMENT ON COLUMN "stripe_webhook_receipts"."organization_id" IS '@access: confidential';
-- Set comment to column: "event_type" on table: "stripe_webhook_receipts"
COMMENT ON COLUMN "stripe_webhook_receipts"."event_type" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "stripe_webhook_receipts"
COMMENT ON COLUMN "stripe_webhook_receipts"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "stripe_webhook_receipts"
COMMENT ON COLUMN "stripe_webhook_receipts"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."slug" IS '@access: confidential';
-- Set comment to column: "name" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."name" IS '@access: confidential';
-- Set comment to column: "category" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."category" IS '@access: confidential';
-- Set comment to column: "description" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."description" IS '@access: confidential';
-- Set comment to column: "sort_order" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."sort_order" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "support_matrix_capabilities"
COMMENT ON COLUMN "support_matrix_capabilities"."deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."id" IS '@access: confidential';
-- Set comment to column: "method_platform_id" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."method_platform_id" IS '@access: confidential';
-- Set comment to column: "capability_id" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."capability_id" IS '@access: confidential';
-- Set comment to column: "status" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."status" IS 'Application-validated: supported, partial, unimplemented, impossible, na, unknown. Partial coverage requires explanatory notes.
@access: confidential';
-- Set comment to column: "notes" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."notes" IS '@access: opaque-restricted';
-- Set comment to column: "needs_verification" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."needs_verification" IS '@access: confidential';
-- Set comment to column: "source_url" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."source_url" IS '@access: opaque-restricted';
-- Set comment to column: "verified_at" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."verified_at" IS '@access: confidential';
-- Set comment to column: "operating_systems" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."operating_systems" IS 'NULL means unassessed; an empty array means unrestricted; otherwise lists eligible operating systems. Coverage restrictions supplement mapping restrictions.
@access: opaque-restricted';
-- Set comment to column: "plan_types" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."plan_types" IS 'NULL means unassessed; an empty array means unrestricted; otherwise lists eligible plan types. Coverage restrictions supplement mapping restrictions.
@access: opaque-restricted';
-- Set comment to column: "conditions" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."conditions" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "support_matrix_coverage"
COMMENT ON COLUMN "support_matrix_coverage"."deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."slug" IS '@access: confidential';
-- Set comment to column: "name" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."name" IS '@access: confidential';
-- Set comment to column: "vendor" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."vendor" IS '@access: confidential';
-- Set comment to column: "description" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."description" IS '@access: confidential';
-- Set comment to column: "plan_notes" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."plan_notes" IS '@access: confidential';
-- Set comment to column: "sort_order" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."sort_order" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "support_matrix_integration_methods"
COMMENT ON COLUMN "support_matrix_integration_methods"."deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."id" IS '@access: confidential';
-- Set comment to column: "integration_method_id" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."integration_method_id" IS '@access: confidential';
-- Set comment to column: "capability_id" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."capability_id" IS '@access: confidential';
-- Set comment to column: "status" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."status" IS 'Application-validated: supported, partial, unimplemented, impossible, na, unknown. Partial coverage requires explanatory notes.
@access: confidential';
-- Set comment to column: "notes" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."notes" IS '@access: opaque-restricted';
-- Set comment to column: "needs_verification" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."needs_verification" IS '@access: confidential';
-- Set comment to column: "source_url" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."source_url" IS '@access: opaque-restricted';
-- Set comment to column: "verified_at" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."verified_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "support_matrix_method_capabilities"
COMMENT ON COLUMN "support_matrix_method_capabilities"."deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."id" IS '@access: confidential';
-- Set comment to column: "integration_method_id" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."integration_method_id" IS '@access: confidential';
-- Set comment to column: "platform_id" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."platform_id" IS '@access: confidential';
-- Set comment to column: "applicability" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."applicability" IS 'Application-validated: unknown, applicable, na. Applicability alone never implies capability coverage.
@access: confidential';
-- Set comment to column: "operating_systems" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."operating_systems" IS 'NULL means unassessed; an empty array means unrestricted; otherwise lists eligible operating systems. Coverage restrictions supplement mapping restrictions.
@access: opaque-restricted';
-- Set comment to column: "plan_types" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."plan_types" IS 'NULL means unassessed; an empty array means unrestricted; otherwise lists eligible plan types. Coverage restrictions supplement mapping restrictions.
@access: opaque-restricted';
-- Set comment to column: "conditions" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."conditions" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "support_matrix_method_platforms"
COMMENT ON COLUMN "support_matrix_method_platforms"."deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."slug" IS '@access: confidential';
-- Set comment to column: "name" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."name" IS '@access: confidential';
-- Set comment to column: "vendor" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."vendor" IS '@access: confidential';
-- Set comment to column: "family" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."family" IS '@access: confidential';
-- Set comment to column: "surface" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."surface" IS '@access: confidential';
-- Set comment to column: "description" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."description" IS '@access: confidential';
-- Set comment to column: "sort_order" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."sort_order" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "support_matrix_platforms"
COMMENT ON COLUMN "support_matrix_platforms"."deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."project_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."provider" IS '@access: confidential';
-- Set comment to column: "reason" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."reason" IS 'The exact agent-facing reason captured at block time, independent of any later risk_results mutation.
@access: opaque-restricted';
-- Set comment to column: "tool_name" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."tool_name" IS '@access: confidential';
-- Set comment to column: "risk_policy_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."risk_policy_id" IS '@access: confidential';
-- Set comment to column: "risk_result_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."risk_result_id" IS 'Optional link to the risk_results finding for this block, backfilled when one is recorded.
@access: confidential';
-- Set comment to column: "chat_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."chat_id" IS '@access: confidential';
-- Set comment to column: "chat_message_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."chat_message_id" IS '@access: confidential';
-- Set comment to column: "feedback" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."feedback" IS '@access: confidential';
-- Set comment to column: "feedback_user_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."feedback_user_id" IS '@access: confidential';
-- Set comment to column: "feedback_at" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."feedback_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."deleted" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "tool_call_blocks"
COMMENT ON COLUMN "tool_call_blocks"."user_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."id" IS '@access: confidential';
-- Set comment to column: "group_id" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."group_id" IS '@access: confidential';
-- Set comment to column: "src_tool_name" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."src_tool_name" IS '@access: confidential';
-- Set comment to column: "confirm" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."confirm" IS '@access: confidential';
-- Set comment to column: "confirm_prompt" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."confirm_prompt" IS '@access: opaque-restricted';
-- Set comment to column: "name" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."name" IS '@access: confidential';
-- Set comment to column: "summary" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."summary" IS '@access: opaque-restricted';
-- Set comment to column: "description" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."description" IS '@access: opaque-restricted';
-- Set comment to column: "tags" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."tags" IS '@access: opaque-restricted';
-- Set comment to column: "summarizer" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."summarizer" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."deleted" IS '@access: confidential';
-- Set comment to column: "src_tool_urn" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."src_tool_urn" IS '@access: confidential';
-- Set comment to column: "title" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."title" IS '@access: confidential';
-- Set comment to column: "read_only_hint" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."read_only_hint" IS '@access: confidential';
-- Set comment to column: "destructive_hint" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."destructive_hint" IS '@access: confidential';
-- Set comment to column: "idempotent_hint" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."idempotent_hint" IS '@access: confidential';
-- Set comment to column: "open_world_hint" on table: "tool_variations"
COMMENT ON COLUMN "tool_variations"."open_world_hint" IS '@access: confidential';
-- Set comment to column: "id" on table: "tool_variations_groups"
COMMENT ON COLUMN "tool_variations_groups"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "tool_variations_groups"
COMMENT ON COLUMN "tool_variations_groups"."project_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "tool_variations_groups"
COMMENT ON COLUMN "tool_variations_groups"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "tool_variations_groups"
COMMENT ON COLUMN "tool_variations_groups"."description" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "tool_variations_groups"
COMMENT ON COLUMN "tool_variations_groups"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "tool_variations_groups"
COMMENT ON COLUMN "tool_variations_groups"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "tool_variations_groups"
COMMENT ON COLUMN "tool_variations_groups"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "tool_variations_groups"
COMMENT ON COLUMN "tool_variations_groups"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."project_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."toolset_id" IS '@access: confidential';
-- Set comment to column: "toolset_version" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."toolset_version" IS '@access: confidential';
-- Set comment to column: "entry_key" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."entry_key" IS '@access: confidential';
-- Set comment to column: "embedding_model" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."embedding_model" IS '@access: confidential';
-- Set comment to column: "embedding_1536" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."embedding_1536" IS '@access: opaque-restricted';
-- Set comment to column: "payload" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."payload" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."deleted" IS '@access: confidential';
-- Set comment to column: "tags" on table: "toolset_embeddings"
COMMENT ON COLUMN "toolset_embeddings"."tags" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "toolset_environments"
COMMENT ON COLUMN "toolset_environments"."id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "toolset_environments"
COMMENT ON COLUMN "toolset_environments"."toolset_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "toolset_environments"
COMMENT ON COLUMN "toolset_environments"."project_id" IS '@access: confidential';
-- Set comment to column: "environment_id" on table: "toolset_environments"
COMMENT ON COLUMN "toolset_environments"."environment_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "toolset_environments"
COMMENT ON COLUMN "toolset_environments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "toolset_environments"
COMMENT ON COLUMN "toolset_environments"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "toolset_origins"
COMMENT ON COLUMN "toolset_origins"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "toolset_origins"
COMMENT ON COLUMN "toolset_origins"."organization_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "toolset_origins"
COMMENT ON COLUMN "toolset_origins"."toolset_id" IS '@access: confidential';
-- Set comment to column: "origin_registry_specifier" on table: "toolset_origins"
COMMENT ON COLUMN "toolset_origins"."origin_registry_specifier" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "toolset_origins"
COMMENT ON COLUMN "toolset_origins"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "toolset_origins"
COMMENT ON COLUMN "toolset_origins"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "toolset_origins"
COMMENT ON COLUMN "toolset_origins"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "toolset_origins"
COMMENT ON COLUMN "toolset_origins"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "toolset_prompts"
COMMENT ON COLUMN "toolset_prompts"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "toolset_prompts"
COMMENT ON COLUMN "toolset_prompts"."project_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "toolset_prompts"
COMMENT ON COLUMN "toolset_prompts"."toolset_id" IS '@access: confidential';
-- Set comment to column: "prompt_history_id" on table: "toolset_prompts"
COMMENT ON COLUMN "toolset_prompts"."prompt_history_id" IS '@access: confidential';
-- Set comment to column: "prompt_template_id" on table: "toolset_prompts"
COMMENT ON COLUMN "toolset_prompts"."prompt_template_id" IS '@access: confidential';
-- Set comment to column: "prompt_name" on table: "toolset_prompts"
COMMENT ON COLUMN "toolset_prompts"."prompt_name" IS '@access: confidential';
-- Set comment to column: "id" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."toolset_id" IS '@access: confidential';
-- Set comment to column: "version" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."version" IS '@access: confidential';
-- Set comment to column: "tool_urns" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."tool_urns" IS '@access: confidential';
-- Set comment to column: "predecessor_id" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."predecessor_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."deleted" IS '@access: confidential';
-- Set comment to column: "resource_urns" on table: "toolset_versions"
COMMENT ON COLUMN "toolset_versions"."resource_urns" IS '@access: opaque-restricted';
-- Set comment to column: "id" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."project_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."slug" IS '@access: confidential';
-- Set comment to column: "description" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."description" IS '@access: opaque-restricted';
-- Set comment to column: "default_environment_slug" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."default_environment_slug" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."deleted" IS '@access: confidential';
-- Set comment to column: "mcp_slug" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."mcp_slug" IS '@access: confidential';
-- Set comment to column: "mcp_is_public" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."mcp_is_public" IS '@access: confidential';
-- Set comment to column: "custom_domain_id" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."custom_domain_id" IS '@access: confidential';
-- Set comment to column: "external_oauth_server_id" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."external_oauth_server_id" IS '@access: confidential';
-- Set comment to column: "oauth_proxy_server_id" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."oauth_proxy_server_id" IS '@access: confidential';
-- Set comment to column: "mcp_enabled" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."mcp_enabled" IS '@access: confidential';
-- Set comment to column: "tool_selection_mode" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."tool_selection_mode" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "tool_variations_group_id" on table: "toolsets"
COMMENT ON COLUMN "toolsets"."tool_variations_group_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "trials"
COMMENT ON COLUMN "trials"."organization_id" IS '@access: confidential';
-- Set comment to column: "tier" on table: "trials"
COMMENT ON COLUMN "trials"."tier" IS '@access: confidential';
-- Set comment to column: "ends_at" on table: "trials"
COMMENT ON COLUMN "trials"."ends_at" IS '@access: confidential';
-- Set comment to column: "converted_at" on table: "trials"
COMMENT ON COLUMN "trials"."converted_at" IS '@access: confidential';
-- Set comment to column: "demoted_at" on table: "trials"
COMMENT ON COLUMN "trials"."demoted_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "trials"
COMMENT ON COLUMN "trials"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "trials"
COMMENT ON COLUMN "trials"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."project_id" IS '@access: confidential';
-- Set comment to column: "definition_slug" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."definition_slug" IS '@access: confidential';
-- Set comment to column: "name" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."name" IS '@access: confidential';
-- Set comment to column: "environment_id" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."environment_id" IS '@access: confidential';
-- Set comment to column: "target_kind" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."target_kind" IS '@access: confidential';
-- Set comment to column: "target_ref" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."target_ref" IS '@access: confidential';
-- Set comment to column: "target_display" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."target_display" IS '@access: confidential';
-- Set comment to column: "config_json" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."config_json" IS '@access: opaque-restricted';
-- Set comment to column: "status" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."status" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "trigger_instances"
COMMENT ON COLUMN "trigger_instances"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."project_id" IS '@access: confidential';
-- Set comment to column: "target_kind" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."target_kind" IS '@access: confidential';
-- Set comment to column: "target_ref" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."target_ref" IS '@access: confidential';
-- Set comment to column: "correlation_id" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."correlation_id" IS '@access: restricted';
-- Set comment to column: "route_to_correlation_id" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."route_to_correlation_id" IS '@access: restricted';
-- Set comment to column: "state" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."state" IS '@access: confidential';
-- Set comment to column: "last_seen_cursor" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."last_seen_cursor" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "trigger_thread_routes"
COMMENT ON COLUMN "trigger_thread_routes"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."id" IS '@access: confidential';
-- Set comment to column: "remote_session_client_id" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."project_id" IS '@access: confidential';
-- Set comment to column: "subject_urn" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."subject_urn" IS '@access: confidential';
-- Set comment to column: "identity_assertion_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."identity_assertion_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "identity_assertion_expires_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."identity_assertion_expires_at" IS '@access: confidential';
-- Set comment to column: "refresh_token_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."refresh_token_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "refresh_expires_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."refresh_expires_at" IS '@access: confidential';
-- Set comment to column: "last_refresh_attempt_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."last_refresh_attempt_at" IS '@access: confidential';
-- Set comment to column: "offline_access_refused_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."offline_access_refused_at" IS '@access: confidential';
-- Set comment to column: "offline_access_request_config_hash" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."offline_access_request_config_hash" IS '@access: secret-restricted';
-- Set comment to column: "created_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."deleted" IS '@access: confidential';
-- Set comment to column: "credential_generation" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."credential_generation" IS '@access: confidential';
-- Set comment to column: "refresh_claim_id" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."refresh_claim_id" IS '@access: confidential';
-- Set comment to column: "upstream_subject_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."upstream_subject_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "nonce_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."nonce_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "credential_config_hash" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."credential_config_hash" IS '@access: secret-restricted';
-- Set comment to column: "observation_status" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."observation_status" IS '@access: confidential';
-- Set comment to column: "observed_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."observed_at" IS '@access: confidential';
-- Set comment to column: "credential_obtained_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."credential_obtained_at" IS '@access: confidential';
-- Set comment to column: "last_refresh_succeeded_at" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."last_refresh_succeeded_at" IS '@access: confidential';
-- Set comment to column: "retry_after" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."retry_after" IS '@access: confidential';
-- Set comment to column: "id" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."id" IS '@access: confidential';
-- Set comment to column: "tunneled_mcp_server_id" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."tunneled_mcp_server_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."name" IS '@access: confidential';
-- Set comment to column: "description" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."description" IS '@access: opaque-restricted';
-- Set comment to column: "is_required" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."is_required" IS '@access: confidential';
-- Set comment to column: "is_secret" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."is_secret" IS '@access: confidential';
-- Set comment to column: "value" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."value" IS '@access: secret-restricted';
-- Set comment to column: "value_from_request_header" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."value_from_request_header" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "tunneled_mcp_server_headers"
COMMENT ON COLUMN "tunneled_mcp_server_headers"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."id" IS 'Stable UUID for the tunneled MCP source. Used by management APIs, dashboard routes, and Redis connection cache keys.
@access: confidential';
-- Set comment to column: "project_id" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."project_id" IS 'Project that owns this tunneled MCP source. All management queries are scoped by project_id.
@access: confidential';
-- Set comment to column: "name" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."name" IS 'User-facing display name for the tunneled MCP source.
@access: confidential';
-- Set comment to column: "key_hash" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."key_hash" IS 'Hash of the one-time tunnel key. Used for future tunnel authentication without storing the plaintext key.
@access: secret-restricted';
-- Set comment to column: "key_prefix" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."key_prefix" IS 'Non-secret prefix of the tunnel key shown in the UI so users can identify which key/source they are using.
@access: secret-restricted';
-- Set comment to column: "status" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."status" IS 'Durable lifecycle state for the source: created, active, or revoked. Live connection state is derived from Redis.
@access: confidential';
-- Set comment to column: "agent_version" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."agent_version" IS 'Last persisted tunnel agent version reported for this source. Per-connection agent versions are stored in Redis.
@access: opaque-restricted';
-- Set comment to column: "last_seen_at" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."last_seen_at" IS 'Most recent persisted heartbeat time for the source, used when Redis liveness data is absent or expired.
@access: confidential';
-- Set comment to column: "created_at" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."created_at" IS 'Time when the tunneled MCP source was created.
@access: confidential';
-- Set comment to column: "updated_at" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."updated_at" IS 'Time when the durable tunneled MCP source record was last updated.
@access: confidential';
-- Set comment to column: "deleted_at" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."deleted_at" IS 'Soft-delete timestamp for the tunneled MCP source. NULL means the source is active.
@access: confidential';
-- Set comment to column: "deleted" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."deleted" IS 'Generated soft-delete flag derived from deleted_at and used by partial indexes.
@access: confidential';
-- Set comment to column: "allow_public" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."allow_public" IS 'Owner consent for anonymous public MCP serving of this source. Double opt-in with mcp_servers.visibility=public, enforced in application code.
@access: confidential';
-- Set comment to column: "resource_identifier" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."resource_identifier" IS 'RFC 9728 protected-resource identifier of the tunneled server, recorded as the RFC 8707 resource on grants and used only for exact-match credential routing. Names a host inside the customer''s private network — never dialed by Gram.
@access: restricted';
-- Set comment to column: "public_request_rate_per_second" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."public_request_rate_per_second" IS '@access: confidential';
-- Set comment to column: "public_request_burst" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."public_request_burst" IS '@access: confidential';
-- Set comment to column: "id" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."project_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."name" IS '@access: confidential';
-- Set comment to column: "slug" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."slug" IS '@access: restricted';
-- Set comment to column: "url" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."url" IS '@access: restricted';
-- Set comment to column: "description" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."description" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "unproxied_mcp_servers"
COMMENT ON COLUMN "unproxied_mcp_servers"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."organization_id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."user_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."provider" IS '@access: confidential';
-- Set comment to column: "external_org_id" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."external_org_id" IS '@access: confidential-pii';
-- Set comment to column: "external_account_uuid" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."external_account_uuid" IS '@access: confidential-pii';
-- Set comment to column: "external_account_id" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."external_account_id" IS '@access: confidential-pii';
-- Set comment to column: "email" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."email" IS '@access: confidential-pii';
-- Set comment to column: "account_type" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."account_type" IS '@access: confidential';
-- Set comment to column: "first_seen_at" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."first_seen_at" IS '@access: confidential';
-- Set comment to column: "last_seen_at" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."last_seen_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."deleted" IS '@access: confidential';
-- Set comment to column: "billing_mode" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."billing_mode" IS '@access: confidential';
-- Set comment to column: "plan_type" on table: "user_accounts"
COMMENT ON COLUMN "user_accounts"."plan_type" IS '@access: confidential';
-- Set comment to column: "id" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."id" IS '@access: confidential';
-- Set comment to column: "user_id" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."user_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."project_id" IS '@access: confidential';
-- Set comment to column: "client_registration_id" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."client_registration_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."toolset_id" IS '@access: confidential';
-- Set comment to column: "oauth_server_issuer" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."oauth_server_issuer" IS '@access: restricted';
-- Set comment to column: "access_token_encrypted" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."access_token_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "refresh_token_encrypted" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."refresh_token_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "token_type" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."token_type" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."expires_at" IS '@access: confidential';
-- Set comment to column: "scopes" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."scopes" IS '@access: opaque-restricted';
-- Set comment to column: "provider_name" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."provider_name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "user_oauth_tokens"
COMMENT ON COLUMN "user_oauth_tokens"."deleted" IS '@access: confidential';
-- Set comment to column: "id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."project_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "client_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_id" IS '@access: restricted';
-- Set comment to column: "client_secret_hash" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_secret_hash" IS '@access: secret-restricted';
-- Set comment to column: "client_name" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_name" IS '@access: opaque-restricted';
-- Set comment to column: "redirect_uris" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."redirect_uris" IS '@access: opaque-restricted';
-- Set comment to column: "client_id_issued_at" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_id_issued_at" IS '@access: confidential';
-- Set comment to column: "client_secret_expires_at" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."deleted" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_uri" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_id_metadata_uri" IS '@access: restricted';
-- Set comment to column: "client_id_metadata_fetched_at" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_id_metadata_fetched_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_cache_expires_at" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_id_metadata_cache_expires_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_etag" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_id_metadata_etag" IS '@access: opaque-restricted';
-- Set comment to column: "token_endpoint_auth_method" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."token_endpoint_auth_method" IS '@access: confidential';
-- Set comment to column: "client_jwks" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_jwks" IS '@access: opaque-restricted';
-- Set comment to column: "client_jwks_uri" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."client_jwks_uri" IS '@access: restricted';
-- Set comment to column: "organization_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."organization_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."project_id" IS '@access: confidential';
-- Set comment to column: "subject_urn" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."subject_urn" IS '@access: confidential';
-- Set comment to column: "user_session_client_id" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."user_session_client_id" IS '@access: confidential';
-- Set comment to column: "remote_set_hash" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."remote_set_hash" IS '@access: confidential';
-- Set comment to column: "consented_at" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."consented_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."deleted" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "user_session_consents"
COMMENT ON COLUMN "user_session_consents"."organization_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."project_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_uri" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."client_id_metadata_uri" IS '@access: restricted';
-- Set comment to column: "created_at" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."deleted" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "user_session_issuer_cimd_clients"
COMMENT ON COLUMN "user_session_issuer_cimd_clients"."organization_id" IS '@access: confidential';
-- Set comment to column: "id" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."project_id" IS '@access: confidential';
-- Set comment to column: "slug" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."slug" IS '@access: confidential';
-- Set comment to column: "authn_challenge_mode" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."authn_challenge_mode" IS '@access: confidential';
-- Set comment to column: "session_duration" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."session_duration" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."deleted" IS '@access: confidential';
-- Set comment to column: "classification" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."classification" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_admission_mode" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."client_id_metadata_admission_mode" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."organization_id" IS '@access: confidential';
-- Set comment to column: "trusted_remote_session_issuer_id" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."trusted_remote_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "attachment_scope" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."attachment_scope" IS '@access: confidential';
-- Set comment to column: "trusted_remote_session_client_id" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."trusted_remote_session_client_id" IS '@access: confidential';
-- Set comment to column: "use_authentication_host" on table: "user_session_issuers"
COMMENT ON COLUMN "user_session_issuers"."use_authentication_host" IS '@access: confidential';
-- Set comment to column: "id" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."project_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "user_session_client_id" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."user_session_client_id" IS '@access: confidential';
-- Set comment to column: "subject_urn" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."subject_urn" IS '@access: restricted';
-- Set comment to column: "jti" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."jti" IS '@access: secret-restricted';
-- Set comment to column: "refresh_token_hash" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."refresh_token_hash" IS '@access: secret-restricted';
-- Set comment to column: "refresh_expires_at" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."refresh_expires_at" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."expires_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."deleted" IS '@access: confidential';
-- Set comment to column: "tool_selection" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."tool_selection" IS '@access: opaque-restricted';
-- Set comment to column: "last_used_at" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."last_used_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."organization_id" IS '@access: confidential';
-- Set comment to column: "authorizer_user_id" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."authorizer_user_id" IS '@access: confidential';
-- Set comment to column: "delegated_grants" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."delegated_grants" IS '@access: opaque-restricted';
-- Set comment to column: "delegated_grants_version" on table: "user_sessions"
COMMENT ON COLUMN "user_sessions"."delegated_grants_version" IS '@access: confidential';
-- Set comment to column: "id" on table: "users"
COMMENT ON COLUMN "users"."id" IS '@access: confidential';
-- Set comment to column: "email" on table: "users"
COMMENT ON COLUMN "users"."email" IS '@access: confidential-pii';
-- Set comment to column: "display_name" on table: "users"
COMMENT ON COLUMN "users"."display_name" IS '@access: confidential-pii';
-- Set comment to column: "photo_url" on table: "users"
COMMENT ON COLUMN "users"."photo_url" IS '@access: confidential-pii';
-- Set comment to column: "admin" on table: "users"
COMMENT ON COLUMN "users"."admin" IS 'Maps to the application''s platform_admin concept: TRUE marks a Gram/Speakeasy platform admin. Distinct from the org-level admin role.
@access: confidential';
-- Set comment to column: "created_at" on table: "users"
COMMENT ON COLUMN "users"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "users"
COMMENT ON COLUMN "users"."updated_at" IS '@access: confidential';
-- Set comment to column: "last_login" on table: "users"
COMMENT ON COLUMN "users"."last_login" IS '@access: confidential';
-- Set comment to column: "workos_id" on table: "users"
COMMENT ON COLUMN "users"."workos_id" IS '@access: confidential-pii';
-- Set comment to column: "deleted_at" on table: "users"
COMMENT ON COLUMN "users"."deleted_at" IS '@access: confidential';
-- Set comment to column: "workos_created_at" on table: "users"
COMMENT ON COLUMN "users"."workos_created_at" IS '@access: confidential';
-- Set comment to column: "workos_updated_at" on table: "users"
COMMENT ON COLUMN "users"."workos_updated_at" IS '@access: confidential';
-- Set comment to column: "workos_deleted_at" on table: "users"
COMMENT ON COLUMN "users"."workos_deleted_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."organization_id" IS '@access: confidential';
-- Set comment to column: "workload_issuer_id" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."workload_issuer_id" IS '@access: confidential';
-- Set comment to column: "subject" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."subject" IS '@access: restricted';
-- Set comment to column: "agent_id" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."agent_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."deleted" IS '@access: confidential';
-- Set comment to column: "match_kind" on table: "workload_agent_assignments"
COMMENT ON COLUMN "workload_agent_assignments"."match_kind" IS '@access: confidential';
-- Set comment to column: "id" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."project_id" IS '@access: confidential';
-- Set comment to column: "workload_issuer_id" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."workload_issuer_id" IS '@access: confidential';
-- Set comment to column: "subject" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."subject" IS '@access: restricted';
-- Set comment to column: "name" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."name" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."deleted" IS '@access: confidential';
-- Set comment to column: "match_kind" on table: "workload_identity_admissions"
COMMENT ON COLUMN "workload_identity_admissions"."match_kind" IS '@access: confidential';
-- Set comment to column: "id" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."project_id" IS '@access: confidential';
-- Set comment to column: "name" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."name" IS '@access: confidential';
-- Set comment to column: "tags" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."tags" IS '@access: opaque-restricted';
-- Set comment to column: "issuer" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."issuer" IS '@access: restricted';
-- Set comment to column: "jwks_uri" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."jwks_uri" IS '@access: restricted';
-- Set comment to column: "metadata" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."metadata" IS '@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."deleted" IS '@access: confidential';
-- Set comment to column: "allow_wildcard_admission" on table: "workload_issuers"
COMMENT ON COLUMN "workload_issuers"."allow_wildcard_admission" IS '@access: confidential';
-- Set comment to column: "id" on table: "workos_organization_syncs"
COMMENT ON COLUMN "workos_organization_syncs"."id" IS '@access: confidential';
-- Set comment to column: "workos_organization_id" on table: "workos_organization_syncs"
COMMENT ON COLUMN "workos_organization_syncs"."workos_organization_id" IS '@access: confidential';
-- Set comment to column: "last_event_id" on table: "workos_organization_syncs"
COMMENT ON COLUMN "workos_organization_syncs"."last_event_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "workos_organization_syncs"
COMMENT ON COLUMN "workos_organization_syncs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "workos_organization_syncs"
COMMENT ON COLUMN "workos_organization_syncs"."updated_at" IS '@access: confidential';
-- Set comment to column: "id" on table: "workos_user_syncs"
COMMENT ON COLUMN "workos_user_syncs"."id" IS '@access: confidential';
-- Set comment to column: "last_event_id" on table: "workos_user_syncs"
COMMENT ON COLUMN "workos_user_syncs"."last_event_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "workos_user_syncs"
COMMENT ON COLUMN "workos_user_syncs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "workos_user_syncs"
COMMENT ON COLUMN "workos_user_syncs"."updated_at" IS '@access: confidential';
-- Set comment to column: "workos_user_id" on table: "workos_user_syncs"
COMMENT ON COLUMN "workos_user_syncs"."workos_user_id" IS '@access: confidential-pii';
