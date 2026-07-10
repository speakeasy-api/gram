# TopUser

Top user by activity

## Example Usage

```typescript
import { TopUser } from "@gram/client/models/components/topuser.js";

let value: TopUser = {
  activityCount: 2147,
  userId: "<id>",
  userType: "internal",
};
```

## Fields

| Field                                                            | Type                                                             | Required                                                         | Description                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `activityCount`                                                  | *number*                                                         | :heavy_check_mark:                                               | Number of messages (session mode) or tool calls (tool_call mode) |
| `userId`                                                         | *string*                                                         | :heavy_check_mark:                                               | User ID (internal or external depending on availability)         |
| `userType`                                                       | [components.UserType](../../models/components/usertype.md)       | :heavy_check_mark:                                               | Type of user ID                                                  |