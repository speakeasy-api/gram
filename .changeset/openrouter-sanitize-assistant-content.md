---
"dashboard": patch
---

Null out assistant-message `content` when `tool_calls` are present before sending to OpenRouter. Fixes the Azure-path OpenAI→Anthropic converter dropping `tool_calls` and producing a dangling `tool_result` on the next turn.
