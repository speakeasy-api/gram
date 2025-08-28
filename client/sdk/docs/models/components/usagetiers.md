# UsageTiers

## Example Usage

```typescript
import { UsageTiers } from "@gram/client/models/components";

let value: UsageTiers = {
  business: {
    basePrice: 754.33,
    includedServers: 650692,
    includedToolCalls: 589937,
    pricePerAdditionalServer: 2826.76,
    pricePerAdditionalToolCall: 8552.2,
  },
  free: {
    basePrice: 2511.29,
    includedServers: 7070,
    includedToolCalls: 806635,
    pricePerAdditionalServer: 9094.85,
    pricePerAdditionalToolCall: 4293.67,
  },
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `business`                                                     | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |
| `free`                                                         | [components.TierLimits](../../models/components/tierlimits.md) | :heavy_check_mark:                                             | N/A                                                            |