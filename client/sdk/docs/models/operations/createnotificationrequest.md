# CreateNotificationRequest

## Example Usage

```typescript
import { CreateNotificationRequest } from "@gram/client/models/operations";

let value: CreateNotificationRequest = {
  createNotificationForm: {
    level: "success",
    title: "<value>",
    type: "user_action",
  },
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `gramSession`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | Session header                                                                         |
| `gramProject`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | project header                                                                         |
| `createNotificationForm`                                                               | [components.CreateNotificationForm](../../models/components/createnotificationform.md) | :heavy_check_mark:                                                                     | N/A                                                                                    |