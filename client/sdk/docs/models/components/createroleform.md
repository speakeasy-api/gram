# CreateRoleForm

## Example Usage

```typescript
import { CreateRoleForm } from "@gram/client/models/components/createroleform.js";

let value: CreateRoleForm = {
  description: "pro pfft hmph woot flashy lovely trivial for at cappelletti",
  grants: [
    {
      scope: "environment:read",
    },
  ],
  name: "<value>",
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `description`                                                        | *string*                                                             | :heavy_check_mark:                                                   | Description of what this role can do.                                |
| `grants`                                                             | [components.RoleGrant](../../models/components/rolegrant.md)[]       | :heavy_check_mark:                                                   | Scope grants to assign.                                              |
| `memberIds`                                                          | *string*[]                                                           | :heavy_minus_sign:                                                   | Optional member IDs to additionally assign to this role on creation. |
| `name`                                                               | *string*                                                             | :heavy_check_mark:                                                   | Display name for the role.                                           |