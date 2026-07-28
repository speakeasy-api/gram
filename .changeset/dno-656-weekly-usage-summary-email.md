---
"server": minor
---

Add a weekly usage summary email for customer billing contacts. Every Monday a Temporal sweep emails each organization's billing alert contact a tokens-under-management digest for the active billing cycle: one line item per TUM component with quantities, plus a comparison against the same elapsed point of the previous cycle and percent-change badges. The TUM token components are now defined once in a `billing.TumComponents` registry that both the ClickHouse billing measure and the email line items derive from, so changes to the TUM definition propagate to billing and reporting in lockstep. Organizations with no usage in either compared window are skipped, and sends are deduplicated per organization and run date.
