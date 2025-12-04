# UsageTiers

## Example Usage

```typescript
import { UsageTiers } from "@gram/client/models/components";

let value: UsageTiers = {
  enterprise: {
    basePrice: 754.33,
    featureBullets: [
      "<value 1>",
      "<value 2>",
    ],
    includedBullets: [
      "<value 1>",
      "<value 2>",
    ],
    includedCredits: 282676,
    includedServers: 855220,
    includedToolCalls: 251129,
    pricePerAdditionalServer: 70.7,
    pricePerAdditionalToolCall: 8066.35,
  },
  free: {
    basePrice: 9094.85,
    featureBullets: [
      "<value 1>",
    ],
    includedBullets: [
      "<value 1>",
      "<value 2>",
    ],
    includedCredits: 303932,
    includedServers: 770876,
    includedToolCalls: 820849,
    pricePerAdditionalServer: 9497.79,
    pricePerAdditionalToolCall: 434.95,
  },
  pro: {
    basePrice: 4213.91,
    featureBullets: [
      "<value 1>",
      "<value 2>",
      "<value 3>",
    ],
    includedBullets: [],
    includedCredits: 43224,
    includedServers: 95928,
    includedToolCalls: 201616,
    pricePerAdditionalServer: 6894.58,
    pricePerAdditionalToolCall: 8500.67,
  },
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `enterprise`                                                   | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |
| `free`                                                         | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |
| `pro`                                                          | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |