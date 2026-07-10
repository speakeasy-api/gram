# SetProductFeatureRequest

## Example Usage

```typescript
import { SetProductFeatureRequest } from "@gram/client/models/operations/setproductfeature.js";

let value: SetProductFeatureRequest = {
  setProductFeatureRequestBody: {
    enabled: false,
    featureName: "tool_io_logs",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `setProductFeatureRequestBody`                                                                     | [components.SetProductFeatureRequestBody](../../models/components/setproductfeaturerequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |