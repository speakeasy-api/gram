---
"server": minor
---

Add a common webhook-trigger abstraction and use it to ship Slack, Linear, and GitHub webhook triggers. A new `HMACScheme` + `WebhookVendor` spec in `triggers/webhook.go` centralizes signature verification (HMAC-SHA256/SHA1, hex/base64, prefix, timestamped templates with replay window) and envelope assembly, so a new webhook source lands as a small vendor file describing its signing scheme, event types, and an ingest function. Slack is rebuilt on the abstraction (no behavior change); Linear (HMAC-SHA256 hex over the bare body, `Linear-Delivery` dedup, comments fold onto their parent issue's conversation) and GitHub (`sha256=`-prefixed hex, `X-GitHub-Delivery` dedup, PR/review/comment correlation onto the PR, pushes onto repo+branch) are added as new triggers. All three share the same default-deny event-type allowlist + CEL filter semantics.
