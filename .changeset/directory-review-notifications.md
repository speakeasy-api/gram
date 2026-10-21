---
"server": minor
---

Tell organization admins about directory changes waiting for a decision. Admins get one email when new directory review items have been quiet for 15 minutes, then a weekly reminder while any stay pending, up to four reminders; the email has counts and a link to the SSO page, never group names or attribute values. Every item that opens or resolves now publishes a `directory_review.opened_v1` or `directory_review.resolved_v1` webhook event in the same transaction, carrying IDs, the kind, its state and the pending count. Admins can turn the emails off per organization with `getDirectoryReviewSettings` and `updateDirectoryReviewSettings`, which are audit logged; webhooks are unaffected. Platform MCP gains `list_directory_review_items` and `resolve_directory_review_item`, which run the dashboard's own review checks and require explicit confirmation.
