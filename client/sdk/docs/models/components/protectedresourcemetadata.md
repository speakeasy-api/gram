# ProtectedResourceMetadata

RFC 9728 OAuth Protected Resource Metadata advertised by a remote MCP server. Only fields the dashboard renders are typed; the RFC allows additional members.

## Example Usage

```typescript
import { ProtectedResourceMetadata } from "@gram/client/models/components/protectedresourcemetadata.js";

let value: ProtectedResourceMetadata = {};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `authorizationServers`                                                | *string*[]                                                            | :heavy_minus_sign:                                                    | Authorization servers that can issue access tokens for this resource. |
| `bearerMethodsSupported`                                              | *string*[]                                                            | :heavy_minus_sign:                                                    | Bearer token presentation methods accepted by the resource server.    |
| `resource`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The resource server's identifier.                                     |
| `resourceDocumentation`                                               | *string*                                                              | :heavy_minus_sign:                                                    | URL of human-readable documentation for the resource server.          |
| `scopesSupported`                                                     | *string*[]                                                            | :heavy_minus_sign:                                                    | Scopes advertised by the resource server.                             |