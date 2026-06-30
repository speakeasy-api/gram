---
"server": minor
"@gram-ai/elements": minor
---

Default to Claude Sonnet 5 (`anthropic/claude-sonnet-5`) for in-app model usage and newly created assistants. The model is added to the allowlist and all model pickers (playground, elements, onboarding). The backend `DefaultChatModel`, the platform-managed assistant, the onboarding assistant default, and the playground/MCP chat surfaces now select Sonnet 5. Specialized models (risk/PromptIntel judges, chat segmentation, embeddings, follow-on suggestions) are unchanged.
