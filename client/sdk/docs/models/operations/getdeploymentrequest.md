# GetDeploymentRequest

## Example Usage

```typescript
import { GetDeploymentRequest } from "@gram/client/models/operations/getdeployment.js";

let value: GetDeploymentRequest = {
  id: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description              |
| ------------- | -------- | ------------------ | ------------------------ |
| `id`          | _string_ | :heavy_check_mark: | The ID of the deployment |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header           |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header           |
| `gramProject` | _string_ | :heavy_minus_sign: | project header           |
