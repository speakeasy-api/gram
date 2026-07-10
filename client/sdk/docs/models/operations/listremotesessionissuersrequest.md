# ListRemoteSessionIssuersRequest

## Example Usage

```typescript
import { ListRemoteSessionIssuersRequest } from "@gram/client/models/operations/listremotesessionissuers.js";

let value: ListRemoteSessionIssuersRequest = {};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `cursor`                         | *string*                         | :heavy_minus_sign:               | Pagination cursor.               |
| `limit`                          | *number*                         | :heavy_minus_sign:               | Page size (default 50, max 100). |
| `gramSession`                    | *string*                         | :heavy_minus_sign:               | Session header                   |
| `gramKey`                        | *string*                         | :heavy_minus_sign:               | API Key header                   |
| `gramProject`                    | *string*                         | :heavy_minus_sign:               | project header                   |