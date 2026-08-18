-- name: GetWebhookEnabledOrg :one
-- Whether one organization is eligible for webhook delivery. Cached per
-- organization for a short TTL, so the common case — an event for an
-- organization that has never enabled webhooks — costs one query per
-- organization per interval rather than one per message.
SELECT id, svix_app_id
FROM organization_metadata
WHERE id = @id
  AND webhooks_enabled IS TRUE
  AND svix_app_id IS NOT NULL
  AND svix_app_id <> ''
  AND disabled_at IS NULL;
