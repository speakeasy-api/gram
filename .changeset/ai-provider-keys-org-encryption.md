---
"server": patch
---

AI integration API keys, Anthropic inference signing secrets, customer model provider keys and provisioned OpenRouter keys can now be encrypted with the organization's own key. They follow the organization's ai-provider-keys transition: organizations stay on the platform key until enrolled, then write both copies, then read only the organization copy. Generated code no longer names the retired plaintext `openrouter_api_keys.key` column.
