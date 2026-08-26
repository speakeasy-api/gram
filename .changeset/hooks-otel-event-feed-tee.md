---
"server": patch
---

OTLP logs received on the deprecated hooks endpoint (/rpc/hooks.otel/v1/logs) are now also republished into the OTel event feed pipeline, so hooks traffic from Claude Code and Codex shows up in the Event Feed while producers migrate to /otel/v1/logs. The tee is best-effort and never affects the hooks response.
