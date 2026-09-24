## Downstream session evidence

All `ChatMessageWriter.WriteExternalWithContentParts` callers, including compliance
imports, enqueue `SessionObserved` in the transcript transaction. Its batched
subscriber reads current ownership from Postgres, enriches the user through the
telemetry resolver, and projects metadata into both `telemetry_logs` (legacy user
metrics and summary views) and `agent_events` (the analytics catalog). Retries
retain the same message and session identities. The event contains no transcript
content. It establishes session activity, never estimated tokens, spend, request
completion, or tool success. Both projections honor the logs feature setting.

Apply the ClickHouse view migration and provision the generated subscription
before enabling the producer. To populate historical session activity, run the
bounded replay command with an explicit project and half-open UTC time window:

```sh
gram replay-session-observations --project-id '<PROJECT_ID>' \
  --from '2026-09-01T00:00:00Z' --to '2026-10-01T00:00:00Z' --limit 1000
```

Continue with the returned `--after` cursor until the command reports no remaining
messages. Replaying the same window is safe for distinct session/message counts;
it does not re-meter storage or trigger policy evaluation. Run this after the
subscriber and view changes are deployed, or the old views will discard the new
row class. The replay excludes deleted chats.

Platform MCP assessment: no new tool or schema is needed. Existing session recall
and analytics tools read the same stores and inherit the session evidence. The
repair command is an operator-only data replay, not a new end-user operation.
Analytics regression tests cover the changed behavior.
