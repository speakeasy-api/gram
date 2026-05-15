---
"server": minor
---

`RiskResult.rule_id` and `RiskResult.description` now follow a consistent shape across every detection source.

`rule_id` is lowercase, kebab-case, with an optional dot-separated category prefix:

- `secret.<rule>` for credentials and secrets (e.g. `secret.anthropic-api-key`)
- `pii.<rule>` for personal, financial, and medical data (e.g. `pii.credit-card`, `pii.medical-license`)
- `shadow-mcp` for unverified MCP tool calls
- `destructive.tool` for MCP tool calls flagged as destructive
- `destructive.<category>.<name>` for destructive shell, git, database, and cloud commands (e.g. `destructive.shell.rm-rf`, `destructive.git.push-force`)
- `prompt-injection` for prompt injection findings

`(source, rule_id)` is the stable identifier downstream consumers should match on. The dotted prefix alone is enough to bucket findings by risk category.

`description` is a short human-readable sentence describing the finding. It never echoes the matched value and is safe to display verbatim.

Historical rows written before this release keep their original `rule_id` and `description` values; a follow-up migration will rewrite them.
