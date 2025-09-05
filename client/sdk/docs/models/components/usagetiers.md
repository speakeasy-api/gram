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
    includedCredits: 589937,
    includedServers: 282676,
    includedToolCalls: 855220,
    pricePerAdditionalCredit: 2511.29,
    pricePerAdditionalServer: 70.7,
    pricePerAdditionalToolCall: 8066.35,
  },
  free: {
    basePrice: 9094.85,
    featureBullets: [
      "<value 1>",
    ],
    includedCredits: 562719,
    includedServers: 303932,
    includedToolCalls: 770876,
    pricePerAdditionalCredit: 8208.49,
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
    includedCredits: 68688,
    includedServers: 43224,
    includedToolCalls: 95928,
    pricePerAdditionalCredit: 2016.16,
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