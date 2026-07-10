# PeriodUsage

## Example Usage

```typescript
import { PeriodUsage } from "@gram/client/models/components/periodusage.js";

let value: PeriodUsage = {
  actualEnabledServerCount: 159907,
  credits: 620048,
  hasActiveSubscription: false,
  includedCredits: 213289,
  includedServers: 182877,
  includedToolCalls: 949307,
  servers: 608367,
  toolCalls: 348095,
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `actualEnabledServerCount`                               | *number*                                                 | :heavy_check_mark:                                       | The number of servers enabled at the time of the request |
| `credits`                                                | *number*                                                 | :heavy_check_mark:                                       | The number of credits used                               |
| `hasActiveSubscription`                                  | *boolean*                                                | :heavy_check_mark:                                       | Whether the project has an active subscription           |
| `includedCredits`                                        | *number*                                                 | :heavy_check_mark:                                       | The number of credits included in the tier               |
| `includedServers`                                        | *number*                                                 | :heavy_check_mark:                                       | The number of servers included in the tier               |
| `includedToolCalls`                                      | *number*                                                 | :heavy_check_mark:                                       | The number of tool calls included in the tier            |
| `servers`                                                | *number*                                                 | :heavy_check_mark:                                       | The number of servers used, according to the Polar meter |
| `toolCalls`                                              | *number*                                                 | :heavy_check_mark:                                       | The number of tool calls used                            |