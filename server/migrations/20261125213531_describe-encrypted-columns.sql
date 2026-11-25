-- Set comment to column: "admin_session_id_enc" on table: "admin_mcp_connections"
COMMENT ON COLUMN "admin_mcp_connections"."admin_session_id_enc" IS 'Linked admin browser session ID, encrypted with the admin app key (GRAM_ADMIN_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "api_key_encrypted" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."api_key_encrypted" IS 'AI provider API key or webhook signing-secret JSON, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is api_key_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "api_key_org_encrypted" on table: "ai_integration_configs"
COMMENT ON COLUMN "ai_integration_configs"."api_key_org_encrypted" IS 'AI provider API key or webhook signing-secret JSON, encrypted with the organization''s data encryption key (DEK) for the ''ai-provider-keys'' use case. SQL cannot read it. The platform-key copy is api_key_encrypted.
@access: secret-restricted';
-- Set comment to column: "token_ciphertext" on table: "assistant_kit_gateway_credentials"
COMMENT ON COLUMN "assistant_kit_gateway_credentials"."token_ciphertext" IS 'Kit gateway credential, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "client_secret_encrypted" on table: "assistant_mcp_oauth_clients"
COMMENT ON COLUMN "assistant_mcp_oauth_clients"."client_secret_encrypted" IS 'Client secret from MCP OAuth dynamic client registration, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "ciphertext" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."ciphertext" IS 'Encrypted copy of one chat_messages row: content, content_raw, tool_calls, tool_outcome_notes, tool_call_summaries and storage_error, encrypted with the organization''s data encryption key (DEK) for the ''chat-transcripts'' use case and bound to the project, chat and message ids. SQL cannot read it; during shadow mode the plaintext chat_messages columns stay the source.
@access: opaque-restricted';
-- Set comment to column: "source_hash" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."source_hash" IS 'Unkeyed SHA-256 digest of the mirrored chat_messages fields, used to detect a stale copy. Not ciphertext: anyone with the plaintext can recompute it.
@access: opaque-restricted';
-- Set comment to column: "content" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content" IS 'Projects on the transcript shadow allowlist also get an organization-key encrypted copy in chat_message_transcripts.ciphertext; during shadow mode this plaintext column stays the source.
@access: opaque-restricted';
-- Set comment to column: "tool_calls" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_calls" IS 'Projects on the transcript shadow allowlist also get an organization-key encrypted copy in chat_message_transcripts.ciphertext; during shadow mode this plaintext column stays the source.
@access: opaque-restricted';
-- Set comment to column: "tool_outcome_notes" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_outcome_notes" IS 'Projects on the transcript shadow allowlist also get an organization-key encrypted copy in chat_message_transcripts.ciphertext; during shadow mode this plaintext column stays the source.
@access: opaque-restricted';
-- Set comment to column: "content_raw" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_raw" IS 'Projects on the transcript shadow allowlist also get an organization-key encrypted copy in chat_message_transcripts.ciphertext; during shadow mode this plaintext column stays the source.
@access: opaque-restricted';
-- Set comment to column: "storage_error" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."storage_error" IS 'Projects on the transcript shadow allowlist also get an organization-key encrypted copy in chat_message_transcripts.ciphertext; during shadow mode this plaintext column stays the source.
@access: opaque-restricted';
-- Set comment to column: "tool_call_summaries" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_call_summaries" IS 'Projects on the transcript shadow allowlist also get an organization-key encrypted copy in chat_message_transcripts.ciphertext; during shadow mode this plaintext column stays the source.
@access: opaque-restricted';
-- Set comment to column: "wrapped_key" on table: "data_encryption_key_wraps"
COMMENT ON COLUMN "data_encryption_key_wraps"."wrapped_key" IS 'The organization''s data encryption key (DEK), wrapped by the KMS key encryption key (KEK) named in external_key_id. Only that KMS key can unwrap it; SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "credentials_encrypted" on table: "device_integration_configs"
COMMENT ON COLUMN "device_integration_configs"."credentials_encrypted" IS 'Provider-specific credential JSON document, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "value" on table: "environment_entries"
COMMENT ON COLUMN "environment_entries"."value" IS 'Environment entry value. When is_secret is true, a non-empty value is encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM) and SQL cannot read it. Otherwise it is plaintext.
@access: secret-restricted';
-- Set comment to column: "encryption_key" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."encryption_key" IS 'Function runner key, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is encryption_key_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "encryption_key_org_encrypted" on table: "functions_access"
COMMENT ON COLUMN "functions_access"."encryption_key_org_encrypted" IS 'Function runner key, encrypted with the organization''s data encryption key (DEK) for the ''function-runner-keys'' use case. SQL cannot read it. The platform-key copy is encryption_key.
@access: secret-restricted';
-- Set comment to column: "private_key_encrypted" on table: "mcp_auth_client_github_apps"
COMMENT ON COLUMN "mcp_auth_client_github_apps"."private_key_encrypted" IS 'Named for an encrypted GitHub App private key, but no Speakeasy code writes this table yet. Treat any value as unreadable secret material.
@access: secret-restricted';
-- Set comment to column: "value" on table: "mcp_auth_client_headers"
COMMENT ON COLUMN "mcp_auth_client_headers"."value" IS 'Static header value. When is_secret is true, a non-empty value is encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM) and SQL cannot read it. Otherwise it is plaintext.
@access: secret-restricted';
-- Set comment to column: "api_key_encrypted" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."api_key_encrypted" IS 'Model provider API key, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is api_key_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "api_key_org_encrypted" on table: "model_provider_keys"
COMMENT ON COLUMN "model_provider_keys"."api_key_org_encrypted" IS 'Model provider API key, encrypted with the organization''s data encryption key (DEK) for the ''ai-provider-keys'' use case. SQL cannot read it. The platform-key copy is api_key_encrypted.
@access: secret-restricted';
-- Set comment to column: "value_encrypted" on table: "network_ingress_certificate_states"
COMMENT ON COLUMN "network_ingress_certificate_states"."value_encrypted" IS 'Certificate, private key or ACME account key, encrypted with the organization''s data encryption key (DEK) for the ''tailscale-gateway-state'' use case. SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "value_encrypted" on table: "network_ingress_node_states"
COMMENT ON COLUMN "network_ingress_node_states"."value_encrypted" IS 'Tailscale node state value, encrypted with the organization''s data encryption key (DEK) for the ''tailscale-gateway-state'' use case. SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "auth_key_encrypted" on table: "network_ingress_nodes"
COMMENT ON COLUMN "network_ingress_nodes"."auth_key_encrypted" IS 'Tailscale auth key for the gateway node, encrypted with the organization''s data encryption key (DEK) for the ''tailscale-gateway-state'' use case. SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "credentials_encrypted" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."credentials_encrypted" IS 'Tailscale OAuth client credential document, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is credentials_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "credentials_org_encrypted" on table: "network_ingresses"
COMMENT ON COLUMN "network_ingresses"."credentials_org_encrypted" IS 'Tailscale OAuth client credential document, encrypted with the organization''s data encryption key (DEK) for the ''tailscale-oauth-clients'' use case. SQL cannot read it. The platform-key copy is credentials_encrypted.
@access: secret-restricted';
-- Set comment to column: "key_encrypted" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."key_encrypted" IS 'Upstream OpenRouter API key, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is key_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "key_org_encrypted" on table: "openrouter_api_keys"
COMMENT ON COLUMN "openrouter_api_keys"."key_org_encrypted" IS 'Upstream OpenRouter API key, encrypted with the organization''s data encryption key (DEK) for the ''ai-provider-keys'' use case. SQL cannot read it. The platform-key copy is key_encrypted.
@access: secret-restricted';
-- Set comment to column: "headers_encrypted" on table: "otel_destinations"
COMMENT ON COLUMN "otel_destinations"."headers_encrypted" IS 'Destination request headers as JSON, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "headers_encrypted" on table: "otel_forwarding_configs"
COMMENT ON COLUMN "otel_forwarding_configs"."headers_encrypted" IS 'Named for encrypted headers, but no Speakeasy code reads or writes this table. Treat any value as unreadable secret material.
@access: secret-restricted';
-- Set comment to column: "marketplace_token_encrypted" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."marketplace_token_encrypted" IS 'Plugin marketplace token, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is marketplace_token_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "marketplace_token_org_encrypted" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."marketplace_token_org_encrypted" IS 'Plugin marketplace token, encrypted with the organization''s data encryption key (DEK) for the ''marketplace-tokens'' use case. SQL cannot read it. The platform-key copy is marketplace_token_encrypted.
@access: secret-restricted';
-- Set comment to column: "value" on table: "remote_mcp_server_headers"
COMMENT ON COLUMN "remote_mcp_server_headers"."value" IS 'Static header value. When is_secret is true, a non-empty value is encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM) and SQL cannot read it. Otherwise it is plaintext.
@access: secret-restricted';
-- Set comment to column: "client_secret_encrypted" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_secret_encrypted" IS 'Upstream OAuth client secret, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is client_secret_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "client_secret_org_encrypted" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_secret_org_encrypted" IS 'Upstream OAuth client secret, encrypted with the organization''s data encryption key (DEK) for the ''oauth-client-secrets'' use case. SQL cannot read it. The platform-key copy is client_secret_encrypted.
@access: secret-restricted';
-- Set comment to column: "access_token_encrypted" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."access_token_encrypted" IS 'Upstream access token, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is access_token_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "access_token_org_encrypted" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."access_token_org_encrypted" IS 'Upstream access token, encrypted with the organization''s data encryption key (DEK) for the ''oauth-session-tokens'' use case. SQL cannot read it. The platform-key copy is access_token_encrypted.
@access: secret-restricted';
-- Set comment to column: "access_token_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."access_token_encrypted" IS 'Upstream OAuth access token, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is access_token_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "refresh_token_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."refresh_token_encrypted" IS 'Upstream OAuth refresh token, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is refresh_token_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "access_token_org_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."access_token_org_encrypted" IS 'Upstream OAuth access token, encrypted with the organization''s data encryption key (DEK) for the ''oauth-session-tokens'' use case. SQL cannot read it. The platform-key copy is access_token_encrypted.
@access: secret-restricted';
-- Set comment to column: "refresh_token_org_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."refresh_token_org_encrypted" IS 'Upstream OAuth refresh token, encrypted with the organization''s data encryption key (DEK) for the ''oauth-session-tokens'' use case. SQL cannot read it. The platform-key copy is refresh_token_encrypted.
@access: secret-restricted';
-- Set comment to column: "payload_encrypted" on table: "risk_execution_evidence"
COMMENT ON COLUMN "risk_execution_evidence"."payload_encrypted" IS 'MCP execution payload, truncated, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "match_encrypted" on table: "risk_finding_evidence"
COMMENT ON COLUMN "risk_finding_evidence"."match_encrypted" IS 'Matched text of an MCP risk finding, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "credentials_encrypted" on table: "slack_directory_connections"
COMMENT ON COLUMN "slack_directory_connections"."credentials_encrypted" IS 'Versioned Slack token bundle, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it.
@access: secret-restricted';
-- Set comment to column: "identity_assertion_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."identity_assertion_encrypted" IS 'Upstream identity assertion, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is identity_assertion_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "refresh_token_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."refresh_token_encrypted" IS 'Upstream refresh token, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is refresh_token_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "upstream_subject_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."upstream_subject_encrypted" IS 'Upstream subject, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is upstream_subject_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "nonce_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."nonce_encrypted" IS 'Original login nonce, encrypted with the app-wide platform key (GRAM_ENCRYPTION_KEY, AES-256-GCM). SQL cannot read it. The organization-key copy is nonce_org_encrypted.
@access: secret-restricted';
-- Set comment to column: "identity_assertion_org_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."identity_assertion_org_encrypted" IS 'Upstream identity assertion, encrypted with the organization''s data encryption key (DEK) for the ''oauth-session-tokens'' use case. SQL cannot read it. The platform-key copy is identity_assertion_encrypted.
@access: secret-restricted';
-- Set comment to column: "refresh_token_org_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."refresh_token_org_encrypted" IS 'Upstream refresh token, encrypted with the organization''s data encryption key (DEK) for the ''oauth-session-tokens'' use case. SQL cannot read it. The platform-key copy is refresh_token_encrypted.
@access: secret-restricted';
-- Set comment to column: "upstream_subject_org_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."upstream_subject_org_encrypted" IS 'Upstream subject, encrypted with the organization''s data encryption key (DEK) for the ''oauth-session-tokens'' use case. SQL cannot read it. The platform-key copy is upstream_subject_encrypted.
@access: secret-restricted';
-- Set comment to column: "nonce_org_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."nonce_org_encrypted" IS 'Original login nonce, encrypted with the organization''s data encryption key (DEK) for the ''oauth-session-tokens'' use case. SQL cannot read it. The platform-key copy is nonce_encrypted.
@access: secret-restricted';
