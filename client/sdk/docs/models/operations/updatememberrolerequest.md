# UpdateMemberRoleRequest

## Example Usage

```typescript
import { UpdateMemberRoleRequest } from "@gram/client/models/operations";

let value: UpdateMemberRoleRequest = {
  updateMemberRoleForm: {
    roleId: "<id>",
    userId: "<id>",
  },
};
```

## Fields

| Field                  | Type                                                                               | Required           | Description    |
| ---------------------- | ---------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`              | _string_                                                                           | :heavy_minus_sign: | API Key header |
| `gramSession`          | _string_                                                                           | :heavy_minus_sign: | Session header |
| `updateMemberRoleForm` | [components.UpdateMemberRoleForm](../../models/components/updatememberroleform.md) | :heavy_check_mark: | N/A            |
