---
"server": patch
---

Forward audited mutations to PostHog as `gram_activity` events. The `gram streams` process now runs a growth-signals handler alongside its existing webhook consumers, so every audited mutation — projects and MCP servers created, security policies written, members invited — reaches PostHog without any service emitting analytics of its own. Uncurated actions pass through under a normalized name so coverage is automatic, and a small exclusion list keeps high-volume noise out.
