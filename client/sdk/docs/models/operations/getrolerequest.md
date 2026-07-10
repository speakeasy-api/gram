# GetRoleRequest

## Example Usage

```typescript
import { GetRoleRequest } from "@gram/client/models/operations/getrole.js";

let value: GetRoleRequest = {
  id: "<id>",
};
```

## Fields

| Field               | Type                | Required            | Description         |
| ------------------- | ------------------- | ------------------- | ------------------- |
| `id`                | *string*            | :heavy_check_mark:  | The ID of the role. |
| `gramKey`           | *string*            | :heavy_minus_sign:  | API Key header      |
| `gramSession`       | *string*            | :heavy_minus_sign:  | Session header      |