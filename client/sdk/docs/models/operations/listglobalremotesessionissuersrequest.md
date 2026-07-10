# ListGlobalRemoteSessionIssuersRequest

## Example Usage

```typescript
import { ListGlobalRemoteSessionIssuersRequest } from "@gram/client/models/operations/listglobalremotesessionissuers.js";

let value: ListGlobalRemoteSessionIssuersRequest = {};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `cursor`                         | *string*                         | :heavy_minus_sign:               | Pagination cursor.               |
| `limit`                          | *number*                         | :heavy_minus_sign:               | Page size (default 50, max 100). |
| `gramSession`                    | *string*                         | :heavy_minus_sign:               | Session header                   |