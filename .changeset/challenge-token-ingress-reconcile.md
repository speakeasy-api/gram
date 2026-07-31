---
"server": patch
---

Saving an OpenAI app-submission verification token now reconciles the custom domain's ingress. Domains provisioned before the challenge feature shipped were missing the `/.well-known/openai-apps-challenge` route, so the token endpoint returned a 404 until an unrelated setting change rebuilt the ingress; setting or clearing the token now triggers that rebuild directly.
