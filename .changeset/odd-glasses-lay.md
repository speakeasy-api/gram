---
"dashboard": minor
---

The catalog "Add to Project" flow now installs servers as Remote MCP servers: one remote MCP server per selected endpoint, a linked private MCP server with a pre-staged default endpoint, OAuth auto-configuration when the upstream supports dynamic client registration, and optional upstream header values collected in the dialog. This replaces the deployment/toolset pipeline (no deployment polling, tool URNs, or fork naming); collection installs and onboarding distribution bundle MCP servers directly, and catalog Added indicators match installed servers by endpoint URL.
