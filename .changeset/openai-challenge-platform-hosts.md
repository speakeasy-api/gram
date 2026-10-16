---
"server": patch
---

The platform hosts now answer `/.well-known/openai-apps-challenge` with the token set in `GRAM_OPENAI_APPS_CHALLENGE_TOKEN`, so the Speakeasy Platform MCP can pass OpenAI's app directory domain verification. Without the token, these hosts still return not found. Custom domains are unchanged.
