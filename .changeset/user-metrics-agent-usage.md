---
"server": patch
---

Count Claude Code and Codex usage in the per-user metrics summary. Both report tokens and cost on their own attributes rather than the generic `gen_ai.usage.*` path the summary read, so a person whose usage came through either surface showed zero tokens and zero spend on their identity page while the cost dashboard billed them in full. The token and cost measures now branch by source the way `attribute_metrics_summaries_mv` does, while still counting the generic rows they always did.
