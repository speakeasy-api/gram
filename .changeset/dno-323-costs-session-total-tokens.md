---
"server": patch
---

Costs and session views now show a correct total token count for AI-coding sessions (Claude Code, etc.). These providers report input and output tokens but never emit `gen_ai.usage.total_tokens`, which previously made per-session and per-user totals read "0 tokens". The telemetry queries now derive the total from input + output when the provider omits an explicit total, while sessions that do carry one are unchanged.
