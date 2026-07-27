---
"server": patch
"dashboard": patch
---

Remote MCP servers now apply configured tool variations to live `tools/list` responses and translate varied aliases back to canonical upstream tool names on `tools/call`. Google Workspace catalog installs use this support to expose rich HTML document import as `create_rich_doc`, while preserving `create_doc` for plain-text documents and documenting the connector's editing tiers.
