---
"server": minor
---

Substitute the OAuth callback URL into the setup guide content that `mcpRegistries.getSetupDocs` returns. Published guides ship with a `{{ gram.oauth.callback_url }}` template key wherever the reader has to register a redirect URI on an upstream provider's OAuth app. The endpoint now replaces that key with this deployment's remote-login callback URL, so `external_markdown` and `speakeasy_markdown` carry a value the reader can paste directly.
