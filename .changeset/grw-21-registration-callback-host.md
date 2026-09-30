---
"server": minor
"dashboard": patch
---

Pin the OAuth redirect URI and client identity URLs that remote session clients register with upstream providers. `GRAM_OUTBOUND_CALLBACK_URL` fixes the origin for existing clients so a server URL change cannot move them, and `GRAM_REGISTRATION_CALLBACK_URL` records a new origin on organization-owned clients created from now on. Remote session clients now report their `callback_url`, and `remoteSessionClients.getNewClientCallbackUrl` returns the redirect URI a new client will register; the dashboard shows these instead of deriving the URL from the server URL.
