---
"server": patch
---

Tool calls now say which product made them, what kind of client it ran in, and which Speakeasy agent it ran as. The `tool_calls` and `skills` analytics datasets gain `product` (named as the AI products catalog names it: claude-code, claude, cowork, codex, cursor and others), `client` (cli, ide, desktop, web, mobile, cloud or slack) and `agent`, shown under the agent's name. `surface`, which mixed the product with where it ran, is still filled and now deprecated in their favour. Claude Code's client comes from its terminal type; hook sources and MCP client names imply theirs. The agent is read only from records Speakeasy itself emits (the MCP gateway's tool call records and the hooks endpoint's), never from attributes a producer sends, and the gateway now names the agent on its records. The new columns fill forward from deployment.
