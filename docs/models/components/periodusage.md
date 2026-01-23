# PeriodUsage

## Example Usage

```typescript
import { PeriodUsage } from "@gram/client/models/components";

let value: PeriodUsage = {
  actualEnabledServerCount: 159907,
  maxServers: 620048,
  maxToolCalls: 836123,
  servers: 213289,
  toolCalls: 182877,
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `actualEnabledServerCount`                               | *number*                                                 | :heavy_check_mark:                                       | The number of servers enabled at the time of the request |
| `maxServers`                                             | *number*                                                 | :heavy_check_mark:                                       | The maximum number of servers allowed                    |
| `maxToolCalls`                                           | *number*                                                 | :heavy_check_mark:                                       | The maximum number of tool calls allowed                 |
| `servers`                                                | *number*                                                 | :heavy_check_mark:                                       | The number of servers used, according to the Polar meter |
| `toolCalls`                                              | *number*                                                 | :heavy_check_mark:                                       | The number of tool calls used                            |