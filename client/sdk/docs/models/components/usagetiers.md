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
    pricePerAdditionalCredit: 70.7,
    pricePerAdditionalServer: 8066.35,
    pricePerAdditionalToolCall: 9094.85,
  },
  free: {
    basePrice: 4293.67,
    featureBullets: [
      "<value 1>",
      "<value 2>",
    ],
    includedBullets: [
      "<value 1>",
    ],
    includedCredits: 770876,
    includedServers: 820849,
    includedToolCalls: 949779,
    pricePerAdditionalCredit: 434.95,
    pricePerAdditionalServer: 4213.91,
    pricePerAdditionalToolCall: 9758.63,
  },
  pro: {
    basePrice: 686.88,
    featureBullets: [],
    includedBullets: [],
    includedCredits: 201616,
    includedServers: 689458,
    includedToolCalls: 850067,
    pricePerAdditionalCredit: 2868.59,
    pricePerAdditionalServer: 8881.92,
    pricePerAdditionalToolCall: 1047.78,
  },
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `enterprise`                                                   | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |
| `free`                                                         | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |
| `pro`                                                          | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |