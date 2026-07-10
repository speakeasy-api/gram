# UpdateRoleRequest

## Example Usage

```typescript
import { UpdateRoleRequest } from "@gram/client/models/operations/updaterole.js";

let value: UpdateRoleRequest = {
  updateRoleForm: {
    id: "<id>",
  },
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `gramKey`                                                              | *string*                                                               | :heavy_minus_sign:                                                     | API Key header                                                         |
| `gramSession`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | Session header                                                         |
| `updateRoleForm`                                                       | [components.UpdateRoleForm](../../models/components/updateroleform.md) | :heavy_check_mark:                                                     | N/A                                                                    |