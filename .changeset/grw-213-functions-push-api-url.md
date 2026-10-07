---
"@gram-ai/functions": patch
---

`gf push` no longer forces the CLI to deploy to `http://localhost:8080`. The CLI now picks the API URL from `--api-url`, `GRAM_API_URL` or your `speakeasy auth` profile, so deploys go to the server you are logged in to.
