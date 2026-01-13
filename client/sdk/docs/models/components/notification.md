# Notification

A notification in the system

## Example Usage

```typescript
import { Notification } from "@gram/client/models/components";

let value: Notification = {
  createdAt: new Date("2025-01-13T08:59:44.748Z"),
  id: "4be69ee3-1733-4d9e-823c-cba00656bccb",
  level: "info",
  projectId: "4a0d3074-82b9-425d-9687-e0746adee2e7",
  title: "<value>",
  type: "system",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `actorUserId`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The user ID of the actor who triggered the notification                                       |
| `archivedAt`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | When the notification was archived                                                            |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the notification was created                                                             |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The notification ID                                                                           |
| `level`                                                                                       | [components.Level](../../models/components/level.md)                                          | :heavy_check_mark:                                                                            | The severity level of the notification                                                        |
| `message`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | The notification message                                                                      |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID                                                                                |
| `resourceId`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the resource this notification relates to                                           |
| `resourceType`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | The type of resource this notification relates to                                             |
| `title`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The notification title                                                                        |
| `type`                                                                                        | [components.Type](../../models/components/type.md)                                            | :heavy_check_mark:                                                                            | The type of notification                                                                      |