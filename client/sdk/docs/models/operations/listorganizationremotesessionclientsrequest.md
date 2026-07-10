# ListOrganizationRemoteSessionClientsRequest

## Example Usage

```typescript
import { ListOrganizationRemoteSessionClientsRequest } from "@gram/client/models/operations/listorganizationremotesessionclients.js";

let value: ListOrganizationRemoteSessionClientsRequest = {
  issuerId: "f5688b6f-71b2-427f-8dbe-8941af3befa3",
};
```

## Fields

| Field                                             | Type                                              | Required                                          | Description                                       |
| ------------------------------------------------- | ------------------------------------------------- | ------------------------------------------------- | ------------------------------------------------- |
| `issuerId`                                        | *string*                                          | :heavy_check_mark:                                | The remote_session_issuer id to list clients for. |
| `cursor`                                          | *string*                                          | :heavy_minus_sign:                                | Pagination cursor.                                |
| `limit`                                           | *number*                                          | :heavy_minus_sign:                                | Page size (default 50, max 100).                  |
| `gramSession`                                     | *string*                                          | :heavy_minus_sign:                                | Session header                                    |
| `gramKey`                                         | *string*                                          | :heavy_minus_sign:                                | API Key header                                    |