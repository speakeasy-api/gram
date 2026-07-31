---
"server": minor
---

Add a weekly usage summary email for customer billing contacts. Every Monday a Temporal sweep emails each organization's billing alert contact their tokens-under-management total for the active billing cycle, with a percent-change badge against the same elapsed point of the previous cycle. The TUM token components are now defined once in a `billing.TumComponents` registry that the ClickHouse billing measure and the email's total both derive from, so changes to the TUM definition propagate to billing and reporting in lockstep. Organizations with no usage in either compared window are skipped, and sends are deduplicated per organization and run date.
