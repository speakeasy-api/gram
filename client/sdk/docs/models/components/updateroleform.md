# UpdateRoleForm

## Example Usage

```typescript
import { UpdateRoleForm } from "@gram/client/models/components/updateroleform.js";

let value: UpdateRoleForm = {
  id: "<id>",
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `addGrants`                                                                                  | [components.RoleGrant](../../models/components/rolegrant.md)[]                               | :heavy_minus_sign:                                                                           | Scope grants to add.                                                                         |
| `description`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | Updated description.                                                                         |
| `id`                                                                                         | *string*                                                                                     | :heavy_check_mark:                                                                           | The ID of the role to update.                                                                |
| `memberIds`                                                                                  | *string*[]                                                                                   | :heavy_minus_sign:                                                                           | Optional member IDs to additionally assign to this role. Existing assignments are preserved. |
| `name`                                                                                       | *string*                                                                                     | :heavy_minus_sign:                                                                           | Updated display name.                                                                        |
| `removeGrants`                                                                               | [components.RoleGrant](../../models/components/rolegrant.md)[]                               | :heavy_minus_sign:                                                                           | Scope grants to remove.                                                                      |