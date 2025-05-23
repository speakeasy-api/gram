# DeleteGlobalVariationRequest

## Example Usage

```typescript
import { DeleteGlobalVariationRequest } from "@gram/client/models/operations";

let value: DeleteGlobalVariationRequest = {
  variationId: "<id>",
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `variationId`                     | *string*                          | :heavy_check_mark:                | The ID of the variation to delete |
| `gramSession`                     | *string*                          | :heavy_minus_sign:                | Session header                    |
| `gramKey`                         | *string*                          | :heavy_minus_sign:                | API Key header                    |
| `gramProject`                     | *string*                          | :heavy_minus_sign:                | project header                    |