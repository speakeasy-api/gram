---
"server": minor
"dashboard": minor
---

Add tokens under management (TUM) billing for enterprise organizations. The billing page now shows enterprise orgs their TUM consumption for the active billing cycle against the contracted monthly allowance, replacing the self-serve usage meters. TUM counts token usage only from agent sessions Gram has stored non-metrics data for (chats, tool calls), excluding OTEL-forwarded token metrics from uninstalled users. Platform admins get an admin-only section on the billing page to set the contracted monthly token limit, an alert email (alerting to follow), and the billing cycle anchor day, backed by the new `usage.getTokensUnderManagement` and `usage.setBillingMetadata` endpoints and a `billing_metadata` table. Contract changes emit `audit_log.billing_metadata_event_v1` audit events.
