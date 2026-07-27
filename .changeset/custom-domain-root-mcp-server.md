---
"server": minor
"dashboard": minor
---

Custom domains can now route their root URL to a default MCP server. Pick one of the domain's MCP endpoints as the default and `https://your-domain.com/` serves that server directly — MCP clients connect at the root and browsers see the installation page — while renaming the endpoint's slug updates the routing automatically. Custom domains can also serve an OpenAI app-submission verification token at `/.well-known/openai-apps-challenge`, so ChatGPT app reviews can verify domain ownership without any changes on your site. Both settings live on the custom domain page; the default server can also be set from an MCP server's own settings.
