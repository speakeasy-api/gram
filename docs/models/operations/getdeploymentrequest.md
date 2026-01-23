# GetDeploymentRequest

## Example Usage

```typescript
import { GetDeploymentRequest } from "@gram/client/models/operations";

let value: GetDeploymentRequest = {
  id: "<id>",
};
```

## Fields

| Field                    | Type                     | Required                 | Description              |
| ------------------------ | ------------------------ | ------------------------ | ------------------------ |
| `id`                     | *string*                 | :heavy_check_mark:       | The ID of the deployment |
| `gramKey`                | *string*                 | :heavy_minus_sign:       | API Key header           |
| `gramSession`            | *string*                 | :heavy_minus_sign:       | Session header           |
| `gramProject`            | *string*                 | :heavy_minus_sign:       | project header           |