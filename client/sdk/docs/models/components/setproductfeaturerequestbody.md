# SetProductFeatureRequestBody

## Example Usage

```typescript
import { SetProductFeatureRequestBody } from "@gram/client/models/components/setproductfeaturerequestbody.js";

let value: SetProductFeatureRequestBody = {
  enabled: false,
  featureName: "tool_io_logs",
};
```

## Fields

| Field                                                            | Type                                                             | Required                                                         | Description                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `enabled`                                                        | *boolean*                                                        | :heavy_check_mark:                                               | Whether the feature should be enabled                            |
| `featureName`                                                    | [components.FeatureName](../../models/components/featurename.md) | :heavy_check_mark:                                               | Name of the feature to update                                    |