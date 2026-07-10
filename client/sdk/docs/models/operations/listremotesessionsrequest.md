# ListRemoteSessionsRequest

## Example Usage

```typescript
import { ListRemoteSessionsRequest } from "@gram/client/models/operations/listremotesessions.js";

let value: ListRemoteSessionsRequest = {};
```

## Fields

| Field                               | Type                                | Required                            | Description                         |
| ----------------------------------- | ----------------------------------- | ----------------------------------- | ----------------------------------- |
| `subjectUrn`                        | *string*                            | :heavy_minus_sign:                  | Exact-match filter on subject URN.  |
| `remoteSessionClientId`             | *string*                            | :heavy_minus_sign:                  | Filter by remote_session_client id. |
| `cursor`                            | *string*                            | :heavy_minus_sign:                  | Pagination cursor.                  |
| `limit`                             | *number*                            | :heavy_minus_sign:                  | Page size (default 50, max 100).    |
| `gramSession`                       | *string*                            | :heavy_minus_sign:                  | Session header                      |
| `gramKey`                           | *string*                            | :heavy_minus_sign:                  | API Key header                      |
| `gramProject`                       | *string*                            | :heavy_minus_sign:                  | project header                      |