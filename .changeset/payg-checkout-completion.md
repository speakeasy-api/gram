---
"server": patch
"dashboard": patch
---

Include all PAYG metered prices in Checkout, preserve resumable billing setup, and apply trial conversion only after confirmed completion. Handle delayed completion and subscription deletion without losing inference-key conversion or post-checkout cleanup, preserve existing Checkout sessions, and show setup actions only to eligible users.

Remove the obsolete platform-admin TUM contract controls and contract price estimator from the billing page.

Show the PAYG risk scanning rate of $0.99 per million tokens scanned and MCP gateway egress rate of $20 per GiB alongside token management pricing.

Place Payment beside PAYG pricing on wider screens and stack them on smaller screens. Prioritize payment setup and recovery above usage, keep healthy subscriptions usage-first, and retain the Organization eyebrow at the top of the billing page.

Use Agent session storage as the billing product name and Stored sessions for usage labels and charts. Rename the Meter usage section to Usage without changing token-based metering or rates.
