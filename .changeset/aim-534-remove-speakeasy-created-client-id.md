---
"dashboard": patch
---

The service account panel no longer shows a disabled "Speakeasy-created" client ID choice marked "Coming soon". Identity providers that support client ID metadata documents, such as Okta, do not accept them for service account (client credentials) clients, so service account clients always use a client ID the provider issues.
