# ListRemoteSessionClientsRequest

## Example Usage

```typescript
import { ListRemoteSessionClientsRequest } from "@gram/client/models/operations/listremotesessionclients.js";

let value: ListRemoteSessionClientsRequest = {};
```

## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `remoteSessionIssuerId`                                 | *string*                                                | :heavy_minus_sign:                                      | Filter to clients registered with this issuer.          |
| `userSessionIssuerId`                                   | *string*                                                | :heavy_minus_sign:                                      | Filter to clients paired with this user_session_issuer. |
| `cursor`                                                | *string*                                                | :heavy_minus_sign:                                      | Pagination cursor.                                      |
| `limit`                                                 | *number*                                                | :heavy_minus_sign:                                      | Page size (default 50, max 100).                        |
| `gramSession`                                           | *string*                                                | :heavy_minus_sign:                                      | Session header                                          |
| `gramKey`                                               | *string*                                                | :heavy_minus_sign:                                      | API Key header                                          |
| `gramProject`                                           | *string*                                                | :heavy_minus_sign:                                      | project header                                          |