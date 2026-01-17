# ListNotificationsResult

Result type for listing notifications

## Example Usage

```typescript
import { ListNotificationsResult } from "@gram/client/models/components";

let value: ListNotificationsResult = {
  notifications: [
    {
      createdAt: new Date("2026-08-21T12:55:14.074Z"),
      id: "f6969768-7110-4e90-a06d-f6ad24691f0f",
      level: "warning",
      projectId: "88ca09c3-5971-4101-8d26-5e8f9c95de50",
      title: "<value>",
      type: "user_action",
    },
  ],
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `nextCursor`                                                         | *string*                                                             | :heavy_minus_sign:                                                   | Cursor for the next page of results                                  |
| `notifications`                                                      | [components.Notification](../../models/components/notification.md)[] | :heavy_check_mark:                                                   | The list of notifications                                            |