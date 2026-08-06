---
"server": patch
---

Slack tool calls that a caller has to fix are no longer reported as server
errors. The Slack Web API answers HTTP 200 with `ok: false` for essentially
every argument, permission, and state problem, so a thread timestamp that names
no thread, a channel the bot was never invited to, a token missing a scope, or
blocks that fail validation all arrived as untyped failures and were logged at
error level by the MCP tool layer. A single misconfigured client emitting a few
hundred of those an hour was enough to hold the component's error-log anomaly
monitor at its threshold for a whole alert window, masking any genuine
regression behind it.

Slack refusals now carry the envelope error code, and the MCP tool boundary
answers a caller-attributed failure with a 400 logged at warn — still recorded
with the upstream code and the tool name, and no longer marking the request span
as errored — while Slack's own failures (`internal_error`, `ratelimited`,
5xx responses) keep error severity. `platform_slack_add_reaction` also treats
`already_reacted` as the successful no-op it is, since the reaction the caller
asked for is already on the message.
