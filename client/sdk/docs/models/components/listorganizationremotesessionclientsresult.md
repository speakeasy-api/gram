# ListOrganizationRemoteSessionClientsResult

Result type for the organization-administrator client listing for a single issuer.

## Example Usage

```typescript
import { ListOrganizationRemoteSessionClientsResult } from "@gram/client/models/components/listorganizationremotesessionclientsresult.js";

let value: ListOrganizationRemoteSessionClientsResult = {
  items: [],
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `items`                                                                                                    | [components.OrganizationRemoteSessionClient](../../models/components/organizationremotesessionclient.md)[] | :heavy_check_mark:                                                                                         | N/A                                                                                                        |
| `nextCursor`                                                                                               | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Cursor for the next page; empty when exhausted.                                                            |