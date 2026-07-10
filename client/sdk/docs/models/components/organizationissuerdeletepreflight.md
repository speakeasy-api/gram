# OrganizationIssuerDeletePreflight

Authoritative impact summary for deleting a remote_session_issuer: how many clients reference it and the names of the MCP servers those clients are attached to.

## Example Usage

```typescript
import { OrganizationIssuerDeletePreflight } from "@gram/client/models/components/organizationissuerdeletepreflight.js";

let value: OrganizationIssuerDeletePreflight = {
  clientCount: 410781,
  mcpServerNames: [
    "<value 1>",
  ],
};
```

## Fields

| Field                                                                     | Type                                                                      | Required                                                                  | Description                                                               |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `clientCount`                                                             | *number*                                                                  | :heavy_check_mark:                                                        | Number of non-deleted remote_session_clients registered with this issuer. |
| `mcpServerNames`                                                          | *string*[]                                                                | :heavy_check_mark:                                                        | Display names of MCP servers attached to this issuer's clients.           |