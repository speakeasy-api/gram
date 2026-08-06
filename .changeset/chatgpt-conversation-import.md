---
"server": minor
"dashboard": patch
---

Import ChatGPT conversations from the OpenAI Compliance Logs Platform. A new `chatgpt_compliance` AI-integration provider polls workspace-scoped `CONVERSATION_MESSAGE` log files (the supported successor to the deprecated stateful conversations endpoint) and persists them as external chats and messages — the same tables and Agent Sessions surface the Anthropic compliance import feeds. The provider is separate from `codex_compliance` because the scopes differ: COSTS files are per API organization while conversation logs are per ChatGPT workspace, so the new config takes a workspace UUID. Includes the workspace-scoped compliance client, Temporal schedule wiring, and a "ChatGPT Conversations" integration card in org settings.
