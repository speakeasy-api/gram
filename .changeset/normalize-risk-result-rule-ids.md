---
"server": minor
"dashboard": minor
---

Normalize `risk_results.rule_id` and `description` into a single canonical contract across every scanner.

`rule_id` is now lowercase kebab-case with optional dot-separated category prefixes:

- `secret.<gitleaks-rule>` for credentials / secrets
- `pii.<presidio-entity>` for personal / financial / medical data
- `shadow-mcp` for unverified MCP tool calls (single rule)
- `destructive.tool` for MCP-annotated destructive tool calls
- `destructive.<category>.<name>` for shell / git / database / cloud command patterns
- `prompt-injection` for prompt injection findings (engine is org-level feature flag)

`description` is now a source-agnostic sentence that interpolates the tool name where useful and never echoes the matched value or internal validator detail. Each scanner routes through a typed `Describe*` builder rather than constructing the strings inline. A regex-grammar guard panics in dev/test when any writer hands back a non-canonical rule id.

Dashboard policy form: prompt-injection becomes a single category-level toggle (no deberta sub-rule); the engine choice is `prompt-injection-use-classifier` per-org feature flag (default regex, opt in for the deberta classifier). The Custom Message field now only renders for block-action policies.

Historical rows with legacy rule ids (`MEDICAL_LICENSE`, `shadow_mcp.unverified_call`, `pi.role-hijack.*`, ...) keep working on read; the dashboard's `humanizeRuleId` fallback renders them legibly. A follow-up migration PR will backfill them.
