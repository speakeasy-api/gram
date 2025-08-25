# TierLimits

## Example Usage

```typescript
import { TierLimits } from "@gram/client/models/components";

let value: TierLimits = {
  basePrice: 567.53,
  includedServers: 481935,
  includedToolCalls: 768996,
  pricePerAdditionalServer: 7392,
  pricePerAdditionalToolCall: 8162.39,
};
```

## Fields

| Field                                         | Type                                          | Required                                      | Description                                   |
| --------------------------------------------- | --------------------------------------------- | --------------------------------------------- | --------------------------------------------- |
| `basePrice`                                   | *number*                                      | :heavy_check_mark:                            | The base price for the tier                   |
| `includedServers`                             | *number*                                      | :heavy_check_mark:                            | The number of servers included in the tier    |
| `includedToolCalls`                           | *number*                                      | :heavy_check_mark:                            | The number of tool calls included in the tier |
| `pricePerAdditionalServer`                    | *number*                                      | :heavy_check_mark:                            | The price per additional server               |
| `pricePerAdditionalToolCall`                  | *number*                                      | :heavy_check_mark:                            | The price per additional tool call            |