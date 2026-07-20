# DeleteRoleRequest

## Example Usage

```typescript
import { DeleteRoleRequest } from "@gram/client/models/operations/deleterole.js";

let value: DeleteRoleRequest = {
  id: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description                   |
| ------------- | -------- | ------------------ | ----------------------------- |
| `id`          | _string_ | :heavy_check_mark: | The ID of the role to delete. |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                |
