# UsageTiers

## Example Usage

```typescript
import { UsageTiers } from "@gram/client/models/components";

let value: UsageTiers = {
  enterprise: {
    basePrice: 754.33,
    descriptionBullets: [
      "<value 1>",
      "<value 2>",
    ],
    includedServers: 589937,
    includedToolCalls: 282676,
    pricePerAdditionalServer: 8552.2,
    pricePerAdditionalToolCall: 2511.29,
  },
  free: {
    basePrice: 70.7,
    descriptionBullets: [
      "<value 1>",
      "<value 2>",
      "<value 3>",
    ],
    includedServers: 909485,
    includedToolCalls: 429367,
    pricePerAdditionalServer: 5627.19,
    pricePerAdditionalToolCall: 3039.32,
  },
  pro: {
    basePrice: 7708.76,
    descriptionBullets: [
      "<value 1>",
      "<value 2>",
      "<value 3>",
    ],
    includedServers: 949779,
    includedToolCalls: 43495,
    pricePerAdditionalServer: 4213.91,
    pricePerAdditionalToolCall: 9758.63,
  },
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `enterprise`                                                   | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |
| `free`                                                         | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |
| `pro`                                                          | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |