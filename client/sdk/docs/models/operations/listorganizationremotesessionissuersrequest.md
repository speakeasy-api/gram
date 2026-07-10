# ListOrganizationRemoteSessionIssuersRequest

## Example Usage

```typescript
import { ListOrganizationRemoteSessionIssuersRequest } from "@gram/client/models/operations/listorganizationremotesessionissuers.js";

let value: ListOrganizationRemoteSessionIssuersRequest = {};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `cursor`                         | *string*                         | :heavy_minus_sign:               | Pagination cursor.               |
| `limit`                          | *number*                         | :heavy_minus_sign:               | Page size (default 50, max 100). |
| `gramSession`                    | *string*                         | :heavy_minus_sign:               | Session header                   |
| `gramKey`                        | *string*                         | :heavy_minus_sign:               | API Key header                   |