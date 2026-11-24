-- Set comment to column: "name" on table: "api_keys"
COMMENT ON COLUMN "api_keys"."name" IS 'Label the customer chose for the key.
@access: opaque-confidential';
-- Set comment to column: "stripe_checkout_idempotency_key" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."stripe_checkout_idempotency_key" IS 'Stripe checkout idempotency key. Encodes the organization, billing anchor, trial fingerprint and possibly a base64 return URL.
@access: restricted';
-- Set comment to column: "content_asset_url" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_asset_url" IS 'Asset path of the stored message content; its file name is a digest of that content.
@access: restricted';
-- Set comment to column: "content_hash" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_hash" IS 'SHA-256 digest of the message content. Reveals when two messages have equal content.
@access: restricted';
-- Set comment to column: "tools" on table: "device_agent_state_reports"
COMMENT ON COLUMN "device_agent_state_reports"."tools" IS 'Per-tool enforcement state. Tool ids are reported by the device agent and stored as sent; layer and outcome fields are normalized enums.
@access: opaque-confidential';
-- Set comment to column: "found" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."found" IS 'Whether the scan found a risk in the anchored content. Security finding.
@access: restricted';
-- Set comment to column: "rule_id" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."rule_id" IS 'Detection rule that matched. Security finding.
@access: restricted';
-- Set comment to column: "start_pos" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."start_pos" IS 'Start offset of the primary match in the scanned content. Security finding.
@access: restricted';
-- Set comment to column: "end_pos" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."end_pos" IS 'End offset of the primary match in the scanned content. Security finding.
@access: restricted';
-- Set comment to column: "confidence" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."confidence" IS 'Detector confidence for the finding. Security finding.
@access: restricted';
-- Set comment to column: "tags" on table: "risk_results"
COMMENT ON COLUMN "risk_results"."tags" IS 'Detector tags for the finding. Security finding.
@access: restricted';
-- Set comment to column: "name" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."name" IS 'User-facing display name for the tunneled MCP source, chosen by the customer.
@access: opaque-confidential';
