# DeleteRoleRequest

## Example Usage

```typescript
import { DeleteRoleRequest } from "@gram/client/models/operations/deleterole.js";

let value: DeleteRoleRequest = {
  id: "<id>",
};
```

## Fields

| Field                         | Type                          | Required                      | Description                   |
| ----------------------------- | ----------------------------- | ----------------------------- | ----------------------------- |
| `id`                          | *string*                      | :heavy_check_mark:            | The ID of the role to delete. |
| `gramKey`                     | *string*                      | :heavy_minus_sign:            | API Key header                |
| `gramSession`                 | *string*                      | :heavy_minus_sign:            | Session header                |