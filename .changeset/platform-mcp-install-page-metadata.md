---
"server": patch
---

Platform MCP `update_mcp_metadata` now patches an MCP server's install-page logo, documentation link and label, and instructions, on any MCP server in the project, alongside the existing rename. Omitted fields are left unchanged and an empty string clears a field; renames still require a server added through the Platform MCP. A logo must be an image owned by the caller's organization, every patch is audit-logged like a dashboard metadata save, and the returned version now also changes when the install page changes, so a stale version is refused. `get_mcp` returns the server's current install page, and `find_mcp` and `get_mcp` return a version for every server. The new `create_org_image_upload_url` tool issues a single-use, 15-minute upload token for a PNG, JPEG, GIF or WebP image of up to 4 MiB that becomes an organization asset, uploaded with `PUT /rpc/organizationAssets.uploadImage` and the token as a bearer header.
