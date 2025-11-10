# SetProductFeatureRequest

## Example Usage

```typescript
import { SetProductFeatureRequest } from "@gram/client/models/operations";

let value: SetProductFeatureRequest = {
  setProductFeatureRequestBody: {
    enabled: false,
    featureName: "logs",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `gramProject`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | project header                                                                                     |
| `setProductFeatureRequestBody`                                                                     | [components.SetProductFeatureRequestBody](../../models/components/setproductfeaturerequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |