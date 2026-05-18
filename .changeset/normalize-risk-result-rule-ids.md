---
"server": minor
---

`RiskResult.rule_id` and `RiskResult.description` now follow a consistent shape across every detection source.

`rule_id` is lowercase, snake_case, with an optional dot-separated category prefix:

- `secret.<rule>` for credentials and secrets (e.g. `secret.anthropic_api_key`)
- `pii.<rule>` for personal, financial, and medical data (e.g. `pii.credit_card`, `pii.medical_license`)
- `shadow_mcp` for unverified MCP tool calls
- `destructive.tool` for MCP tool calls flagged as destructive
- `destructive.<category>.<name>` for destructive shell, git, database, and cloud commands (e.g. `destructive.shell.rm_rf`, `destructive.git.push_force`)
- `prompt_injection` for prompt injection findings

`(source, rule_id)` is the stable identifier downstream consumers should match on. The dotted prefix alone is enough to bucket findings by risk category.

`description` is a short human-readable sentence describing the finding. It never echoes the matched value and is safe to display verbatim.

Historical rows written before this release keep their original `rule_id` and `description` values; a follow-up migration will rewrite them.
