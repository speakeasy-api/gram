---
"dashboard": minor
"server": patch
---

Organization admins can enable Enterprise Managed Authorization from Speakeasy's own page under IDP and SSO › Identity providers. Its card there summarizes the issuer's status and URI and opens the page, which has separate sections for status, the issuer URI to register with supported services, signing keys (replace a compromised key, or finish setting up a new one after a key was withdrawn), and disabling it with an explanation of the effect. If a replacement key can't be set up, the page says so instead of reporting success. The issuer now also checks that it belongs to the deployment serving it before it signs or can be enabled.
