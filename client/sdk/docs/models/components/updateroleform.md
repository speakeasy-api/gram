# UpdateRoleForm

## Example Usage

```typescript
import { UpdateRoleForm } from "@gram/client/models/components/updateroleform.js";

let value: UpdateRoleForm = {
  id: "<id>",
};
```

## Fields

| Field          | Type                                                           | Required           | Description                                                                                  |
| -------------- | -------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------- |
| `addGrants`    | [components.RoleGrant](../../models/components/rolegrant.md)[] | :heavy_minus_sign: | Scope grants to add.                                                                         |
| `description`  | _string_                                                       | :heavy_minus_sign: | Updated description.                                                                         |
| `id`           | _string_                                                       | :heavy_check_mark: | The ID of the role to update.                                                                |
| `memberIds`    | _string_[]                                                     | :heavy_minus_sign: | Optional member IDs to additionally assign to this role. Existing assignments are preserved. |
| `name`         | _string_                                                       | :heavy_minus_sign: | Updated display name.                                                                        |
| `removeGrants` | [components.RoleGrant](../../models/components/rolegrant.md)[] | :heavy_minus_sign: | Scope grants to remove.                                                                      |
