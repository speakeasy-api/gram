# TierLimits

## Example Usage

```typescript
import { TierLimits } from "@gram/client/models/components";

let value: TierLimits = {
  basePrice: 567.53,
  descriptionBullets: [
    "<value 1>",
  ],
  includedServers: 768996,
  includedToolCalls: 739200,
  pricePerAdditionalServer: 8162.39,
  pricePerAdditionalToolCall: 5275.46,
};
```

## Fields

| Field                                         | Type                                          | Required                                      | Description                                   |
| --------------------------------------------- | --------------------------------------------- | --------------------------------------------- | --------------------------------------------- |
| `basePrice`                                   | *number*                                      | :heavy_check_mark:                            | The base price for the tier                   |
| `descriptionBullets`                          | *string*[]                                    | :heavy_check_mark:                            | The description bullets of the tier           |
| `includedServers`                             | *number*                                      | :heavy_check_mark:                            | The number of servers included in the tier    |
| `includedToolCalls`                           | *number*                                      | :heavy_check_mark:                            | The number of tool calls included in the tier |
| `pricePerAdditionalServer`                    | *number*                                      | :heavy_check_mark:                            | The price per additional server               |
| `pricePerAdditionalToolCall`                  | *number*                                      | :heavy_check_mark:                            | The price per additional tool call            |