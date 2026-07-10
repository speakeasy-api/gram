# UpdateMemberRoleForm

## Example Usage

```typescript
import { UpdateMemberRoleForm } from "@gram/client/models/components";

let value: UpdateMemberRoleForm = {
  roleId: "<id>",
  userId: "<id>",
};
```

## Fields

| Field                      | Type                       | Required                   | Description                |
| -------------------------- | -------------------------- | -------------------------- | -------------------------- |
| `roleId`                   | *string*                   | :heavy_check_mark:         | The new role ID to assign. |
| `userId`                   | *string*                   | :heavy_check_mark:         | The user ID to update.     |