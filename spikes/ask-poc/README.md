# Ask POC

Tests whether Claude Code resumes on its own after an approval, using only
tool results and the Monitor tool. Nothing is installed in Claude Code except
a plugin that points at two local MCP servers.

1. `external-slack` (port 4701) has a mock `send_message`. The first call is
   rejected: "Blocked by Ask policy ... call request_approval with
   blocked_call_id call_N".
2. `approvals` (port 4702) has `request_approval`. It files an ask, logs a
   mock Slack bot notification, and returns a status URL plus a watcher script.
   It tells the agent to save the script and run it with the Monitor tool.
3. The ask is approved automatically after `ASK_POC_AUTO_APPROVE_SECONDS`
   (default 10). The watcher prints `ASK APPROVED` and exits. That line should
   wake the agent, which retries the identical `send_message`, and this time
   the call succeeds.

## Files

- `server.mjs`: both MCP servers, shared in-memory state, no dependencies.
- `ask-watch-v1.sh`: the watcher script. Its text is embedded in the
  `request_approval` response.
- `ask-poc.sh`: start, stop, restart, status, logs, approve, deny, reset.
- `plugin/`: a Claude Code plugin whose `.mcp.json` points at both servers.

- `control-mcp.mjs`: a stdio MCP server, `ask-poc-control`, that the Ask POC
  Lab artifact calls as `host:ask-poc-control`. It restarts the mock
  servers, reads the log, and approves or denies asks. It is registered at user
  scope with
  `claude mcp add --scope user ask-poc-control -- <node> <this dir>/control-mcp.mjs`,
  and removed with `claude mcp remove ask-poc-control -s user`.
- `run-claude.sh` and `run-codex.sh`: start each agent in
  `~/.cache/ask-spike/playground` with the mock servers configured, real Slack
  tools off, and the default prompt sent. Pass a prompt in quotes to override
  it.

## Knobs

| Env                            | Default | Meaning                                                                                                          |
| ------------------------------ | ------- | ---------------------------------------------------------------------------------------------------------------- |
| `ASK_POC_AUTO_APPROVE_SECONDS` | `10`    | Seconds until auto-approve. `0` means decide by hand with `./ask-poc.sh approve <ask_id>`                        |
| `ASK_POC_ERROR_STYLE`          | `tool`  | `tool` returns a tool result with `isError`. `jsonrpc` returns JSON-RPC error -32003, like the Speakeasy gateway |
| `ASK_POC_TTL_SECONDS`          | `1800`  | When a pending ask expires                                                                                       |

## Endpoints

- `GET  http://127.0.0.1:4702/asks/<id>`: `pending`, `approved`, `denied` or `expired`
- `POST http://127.0.0.1:4702/asks/<id>/approve` or `/deny`
- `GET  http://127.0.0.1:4702/health`: blocked calls, asks and sent messages
- `GET  http://127.0.0.1:4701/sent`: messages the mock "sent"

## Safety

Run Claude Code with `--disallowedTools 'mcp__*Slack*'`. That removes real
Slack connector tools (for example the External Slack plugin) so the model
cannot pick a real channel, and keeps the lowercase mock
`mcp__plugin_ask-poc_external-slack__send_message`.
