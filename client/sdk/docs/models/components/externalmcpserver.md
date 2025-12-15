# ExternalMCPServer

An MCP server from an external registry

## Example Usage

```typescript
import { ExternalMCPServer } from "@gram/client/models/components";

let value: ExternalMCPServer = {
  description: "yahoo lest joshingly forgery",
  name: "io.modelcontextprotocol.anonymous/exa",
  registryId: "a8daedbf-8518-49c9-b7dd-8d561d9f86cf",
  version: "1.0.0",
};
```

## Fields

| Field                                                             | Type                                                              | Required                                                          | Description                                                       | Example                                                           |
| ----------------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------- |
| `description`                                                     | *string*                                                          | :heavy_check_mark:                                                | Description of what the server does                               |                                                                   |
| `iconUrl`                                                         | *string*                                                          | :heavy_minus_sign:                                                | URL to the server's icon                                          |                                                                   |
| `name`                                                            | *string*                                                          | :heavy_check_mark:                                                | Server name in reverse-DNS format (e.g., 'io.github.user/server') | io.modelcontextprotocol.anonymous/exa                             |
| `registryId`                                                      | *string*                                                          | :heavy_check_mark:                                                | ID of the registry this server came from                          |                                                                   |
| `title`                                                           | *string*                                                          | :heavy_minus_sign:                                                | Display name for the server                                       |                                                                   |
| `version`                                                         | *string*                                                          | :heavy_check_mark:                                                | Semantic version of the server                                    | 1.0.0                                                             |