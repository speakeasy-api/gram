---
"server": patch
---

Stop LiteLLM-proxied agent sessions from persisting each assistant turn twice,
and label Claude Code sessions as Claude Code. The proxy reports the completion
the moment the model returns it and the agent's own hook stream reports the same
text when the turn ends; prompts already collapsed onto one row through the
session's native marker, but assistant turns share no identity across the two
observers, so both rows survived. A proxied assistant turn is now dropped for
sessions a Claude hook stream already captured (cost and telemetry for the
proxied call are unaffected). Separately, the bare `claude` adapter slug the
hooks binary sends now resolves to the `claude-code` surface even when a session
has no OpenTelemetry stream, instead of colliding with the `claude` the Anthropic
compliance import writes for Claude Chat Desktop and being displayed as that
surface.
