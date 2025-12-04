# TierLimits

## Example Usage

```typescript
import { TierLimits } from "@gram/client/models/components";

let value: TierLimits = {
  basePrice: 567.53,
  featureBullets: [
    "<value 1>",
  ],
  includedBullets: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  includedCredits: 739200,
  includedServers: 816239,
  includedToolCalls: 527546,
  pricePerAdditionalServer: 6766.15,
  pricePerAdditionalToolCall: 2929.08,
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `addOnBullets`                                                                           | *string*[]                                                                               | :heavy_minus_sign:                                                                       | Add-on items bullets of the tier (optional)                                              |
| `basePrice`                                                                              | *number*                                                                                 | :heavy_check_mark:                                                                       | The base price for the tier                                                              |
| `featureBullets`                                                                         | *string*[]                                                                               | :heavy_check_mark:                                                                       | Key feature bullets of the tier                                                          |
| `includedBullets`                                                                        | *string*[]                                                                               | :heavy_check_mark:                                                                       | Included items bullets of the tier                                                       |
| `includedCredits`                                                                        | *number*                                                                                 | :heavy_check_mark:                                                                       | The number of credits included in the tier for playground and other dashboard activities |
| `includedServers`                                                                        | *number*                                                                                 | :heavy_check_mark:                                                                       | The number of servers included in the tier                                               |
| `includedToolCalls`                                                                      | *number*                                                                                 | :heavy_check_mark:                                                                       | The number of tool calls included in the tier                                            |
| `pricePerAdditionalServer`                                                               | *number*                                                                                 | :heavy_check_mark:                                                                       | The price per additional server                                                          |
| `pricePerAdditionalToolCall`                                                             | *number*                                                                                 | :heavy_check_mark:                                                                       | The price per additional tool call                                                       |