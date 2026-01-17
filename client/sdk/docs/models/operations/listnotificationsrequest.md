# ListNotificationsRequest

## Example Usage

```typescript
import { ListNotificationsRequest } from "@gram/client/models/operations";

let value: ListNotificationsRequest = {};
```

## Fields

| Field                                                                           | Type                                                                            | Required                                                                        | Description                                                                     |
| ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `archived`                                                                      | *boolean*                                                                       | :heavy_minus_sign:                                                              | Filter by archived status. If not provided, returns non-archived notifications. |
| `limit`                                                                         | *number*                                                                        | :heavy_minus_sign:                                                              | Maximum number of notifications to return                                       |
| `cursor`                                                                        | *string*                                                                        | :heavy_minus_sign:                                                              | Cursor for pagination (notification ID)                                         |
| `gramSession`                                                                   | *string*                                                                        | :heavy_minus_sign:                                                              | Session header                                                                  |
| `gramProject`                                                                   | *string*                                                                        | :heavy_minus_sign:                                                              | project header                                                                  |