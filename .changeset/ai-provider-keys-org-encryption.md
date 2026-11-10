---
"server": patch
---

AI provider and model credentials (AI integration API keys and Anthropic webhook signing secrets, customer model provider keys, and provisioned OpenRouter keys) can move to per-organization encryption under a dedicated `ai-provider-keys` data encryption key. The `ai-provider-keys-org-encryption` flag moves one organization at a time: its credentials are also sealed under its own key, reads use that copy, and existing rows gain it on their next read. The application-key copy is kept, so turning the flag off restores the previous behaviour. The deprecated plaintext `openrouter_api_keys.key` column is no longer read by generated code.
