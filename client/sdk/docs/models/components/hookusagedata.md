# HookUsageData

Token and cost usage payload.

## Example Usage

```typescript
import { HookUsageData } from "@gram/client/models/components/hookusagedata.js";

let value: HookUsageData = {};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `cacheReadTokens`                                          | *number*                                                   | :heavy_minus_sign:                                         | Cache read token count.                                    |
| `cacheWriteTokens`                                         | *number*                                                   | :heavy_minus_sign:                                         | Cache write token count.                                   |
| `cost`                                                     | *number*                                                   | :heavy_minus_sign:                                         | Reported cost.                                             |
| `inputTokens`                                              | *number*                                                   | :heavy_minus_sign:                                         | Input token count.                                         |
| `loopCount`                                                | *number*                                                   | :heavy_minus_sign:                                         | Agent loop count, when reported.                           |
| `outputTokens`                                             | *number*                                                   | :heavy_minus_sign:                                         | Output token count.                                        |
| `status`                                                   | *string*                                                   | :heavy_minus_sign:                                         | Provider-reported usage or session status, when available. |