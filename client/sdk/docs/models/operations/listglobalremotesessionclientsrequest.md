# ListGlobalRemoteSessionClientsRequest

## Example Usage

```typescript
import { ListGlobalRemoteSessionClientsRequest } from "@gram/client/models/operations/listglobalremotesessionclients.js";

let value: ListGlobalRemoteSessionClientsRequest = {
  remoteSessionIssuerId: "579ac7d1-35dc-43a1-b4f4-3a9dcc6bc8e1",
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `remoteSessionIssuerId`                                  | *string*                                                 | :heavy_check_mark:                                       | The global remote_session_issuer id to list clients for. |
| `cursor`                                                 | *string*                                                 | :heavy_minus_sign:                                       | Pagination cursor.                                       |
| `limit`                                                  | *number*                                                 | :heavy_minus_sign:                                       | Page size (default 50, max 100).                         |
| `gramSession`                                            | *string*                                                 | :heavy_minus_sign:                                       | Session header                                           |