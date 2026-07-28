# OrganizationMcpServer

An MCP server attached to a remote_session_client, with the fields the org-admin UI needs to display and link to it.

## Example Usage

```typescript
import { OrganizationMcpServer } from "@gram/client/models/components/organizationmcpserver.js";

let value: OrganizationMcpServer = {
  id: "0fb6cad1-13b8-449c-96ee-fc8137adde9c",
  projectId: "22d0736d-aa7e-4d89-9f00-2832a955c250",
};
```

## Fields

| Field         | Type     | Required           | Description                                                               |
| ------------- | -------- | ------------------ | ------------------------------------------------------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The mcp_server id.                                                        |
| `name`        | _string_ | :heavy_minus_sign: | The MCP server name; empty when unset (display falls back to the URL).    |
| `projectId`   | _string_ | :heavy_check_mark: | The owning project id.                                                    |
| `projectSlug` | _string_ | :heavy_minus_sign: | The owning project's slug, for linking to the MCP server in its project.  |
| `slug`        | _string_ | :heavy_minus_sign: | The MCP server slug.                                                      |
| `url`         | _string_ | :heavy_minus_sign: | The remote MCP server URL; empty for non-remote (toolset-backed) servers. |
