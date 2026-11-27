---
"@speakeasy-api/functions": patch
---

After a deploy, the browser link to create an MCP server opens the dashboard on the Speakeasy host your CLI profile uses, instead of always `https://app.getgram.ai`. This needs a CLI whose `speakeasy whoami --json` output includes `api_url`; with an older CLI the link still opens `https://app.getgram.ai`.
