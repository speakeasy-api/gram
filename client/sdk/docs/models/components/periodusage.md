# PeriodUsage

## Example Usage

```typescript
import { PeriodUsage } from "@gram/client/models/components";

let value: PeriodUsage = {
  maxServers: 159907,
  maxToolCalls: 620048,
  servers: 836123,
  toolCalls: 213289,
};
```

## Fields

| Field                                    | Type                                     | Required                                 | Description                              |
| ---------------------------------------- | ---------------------------------------- | ---------------------------------------- | ---------------------------------------- |
| `maxServers`                             | *number*                                 | :heavy_check_mark:                       | The maximum number of servers allowed    |
| `maxToolCalls`                           | *number*                                 | :heavy_check_mark:                       | The maximum number of tool calls allowed |
| `servers`                                | *number*                                 | :heavy_check_mark:                       | The number of servers used               |
| `toolCalls`                              | *number*                                 | :heavy_check_mark:                       | The number of tool calls used            |