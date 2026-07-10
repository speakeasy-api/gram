# UpdateMemberRolesForm

## Example Usage

```typescript
import { UpdateMemberRolesForm } from "@gram/client/models/components/updatememberrolesform.js";

let value: UpdateMemberRolesForm = {
  roleIds: [
    "<value 1>",
    "<value 2>",
  ],
  userId: "<id>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `roleIds`                                                       | *string*[]                                                      | :heavy_check_mark:                                              | The role IDs to assign. Replaces all existing role assignments. |
| `userId`                                                        | *string*                                                        | :heavy_check_mark:                                              | The user ID to update.                                          |