# CreateNotificationForm

Form for creating a new notification

## Example Usage

```typescript
import { CreateNotificationForm } from "@gram/client/models/components";

let value: CreateNotificationForm = {
  level: "error",
  title: "<value>",
  type: "user_action",
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `level`                                                                                          | [components.CreateNotificationFormLevel](../../models/components/createnotificationformlevel.md) | :heavy_check_mark:                                                                               | The severity level of the notification                                                           |
| `message`                                                                                        | *string*                                                                                         | :heavy_minus_sign:                                                                               | The notification message                                                                         |
| `resourceId`                                                                                     | *string*                                                                                         | :heavy_minus_sign:                                                                               | The ID of the resource this notification relates to                                              |
| `resourceType`                                                                                   | *string*                                                                                         | :heavy_minus_sign:                                                                               | The type of resource this notification relates to                                                |
| `title`                                                                                          | *string*                                                                                         | :heavy_check_mark:                                                                               | The notification title                                                                           |
| `type`                                                                                           | [components.CreateNotificationFormType](../../models/components/createnotificationformtype.md)   | :heavy_check_mark:                                                                               | The type of notification                                                                         |