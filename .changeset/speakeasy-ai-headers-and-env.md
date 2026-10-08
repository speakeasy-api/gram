---
"server": minor
"dashboard": minor
---

Rename the request headers and environment variables customers set to Speakeasy AI names. Every Gram name keeps working as a deprecated alias.

- **Request headers:** the API accepts `X-Speakeasy-AI-Key`, `X-Speakeasy-AI-Project`, `X-Speakeasy-AI-Session`, `X-Speakeasy-AI-Chat-Session`, `X-Speakeasy-AI-Environment`, `X-Speakeasy-AI-Mode`, `X-Speakeasy-AI-User-Email` and `X-Speakeasy-AI-Device-Serial`, `-Hostname` and `-Environment`. Each is the canonical name of the `Gram-*` header it replaces, and wins when a request sends both. LiteLLM ingest also reads `x-speakeasy-ai-session-id` and `x-speakeasy-ai-agent-provider`, `-session-id` and `-turn-id` before the `x-gram-*` names.
- **Hooks:** `speakeasy-hooks` reads every setting from `SPEAKEASY_AI_HOOKS_*`, for example `SPEAKEASY_AI_HOOKS_API_KEY`, and falls back to the matching `GRAM_HOOKS_*` name when the new one is unset. The plugin bootstrap reads `SPEAKEASY_AI_HOOKS_HOME` before `GRAM_HOOKS_HOME`. New credential caches go to `$XDG_CONFIG_HOME/speakeasy-ai/hooks-auth.env`; an existing cache at `$XDG_CONFIG_HOME/gram/hooks-auth.env` stays in use.
- **Generated plugins:** Cursor, OpenCode and Codex plugins read the API key from `SPEAKEASY_AI_API_KEY`, and Claude Code plugins prompt for it under that name. OpenCode plugins still read `GRAM_API_KEY` when `SPEAKEASY_AI_API_KEY` is unset; Cursor and Codex cannot fall back, so set `SPEAKEASY_AI_API_KEY` after updating those plugins. Codex telemetry and the OpenCode LiteLLM attribution use the new header names.
- **Install snippets:** the hosted MCP install page, the dashboard setup, hooks and LiteLLM pages, and the SDK samples show only the new names. The MCP install page uses `X-Speakeasy-AI-Environment` with `SPEAKEASY_AI_ENVIRONMENT` and `SPEAKEASY_AI_API_KEY`.
- **Elements:** the `environmentSlug` option replaces `gramEnvironment`, which still works when `environmentSlug` is not set.
