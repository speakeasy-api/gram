---
"dashboard": patch
"server": patch
---

The consent page's tool list loads after a sign-in through the organization's identity provider. It failed with "Couldn't load this server's tools" because its requests left out the sign-in browser cookie.
