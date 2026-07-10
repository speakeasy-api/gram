# ListOrganizationRemoteSessionClientSessionsRequest

## Example Usage

```typescript
import { ListOrganizationRemoteSessionClientSessionsRequest } from "@gram/client/models/operations/listorganizationremotesessionclientsessions.js";

let value: ListOrganizationRemoteSessionClientSessionsRequest = {
  clientId: "ac32dbcc-b39b-4288-818e-2e039aae288c",
};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `clientId`                       | *string*                         | :heavy_check_mark:               | The remote_session_client id.    |
| `cursor`                         | *string*                         | :heavy_minus_sign:               | Pagination cursor.               |
| `limit`                          | *number*                         | :heavy_minus_sign:               | Page size (default 50, max 100). |
| `gramSession`                    | *string*                         | :heavy_minus_sign:               | Session header                   |
| `gramKey`                        | *string*                         | :heavy_minus_sign:               | API Key header                   |