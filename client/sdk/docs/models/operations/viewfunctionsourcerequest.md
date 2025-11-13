# ViewFunctionSourceRequest

## Example Usage

```typescript
import { ViewFunctionSourceRequest } from "@gram/client/models/operations";

let value: ViewFunctionSourceRequest = {
  id: "<id>",
  projectId: "<id>",
};
```

## Fields

| Field                                    | Type                                     | Required                                 | Description                              |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| `id`                                     | *string*                                 | :heavy_check_mark:                       | The ID of the asset to view              |
| `projectId`                              | *string*                                 | :heavy_check_mark:                       | The project ID that the asset belongs to |
| `gramKey`                                | *string*                                 | :heavy_minus_sign:                       | API Key header                           |
| `gramSession`                            | *string*                                 | :heavy_minus_sign:                       | Session header                           |