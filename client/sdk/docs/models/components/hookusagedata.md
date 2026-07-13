# HookUsageData

Token and cost usage payload.

## Example Usage

```typescript
import { HookUsageData } from "@gram/client/models/components/hookusagedata.js";

let value: HookUsageData = {};
```

## Fields

| Field              | Type     | Required           | Description                                                |
| ------------------ | -------- | ------------------ | ---------------------------------------------------------- |
| `cacheReadTokens`  | _number_ | :heavy_minus_sign: | Cache read token count.                                    |
| `cacheWriteTokens` | _number_ | :heavy_minus_sign: | Cache write token count.                                   |
| `cost`             | _number_ | :heavy_minus_sign: | Reported cost.                                             |
| `inputTokens`      | _number_ | :heavy_minus_sign: | Input token count.                                         |
| `loopCount`        | _number_ | :heavy_minus_sign: | Agent loop count, when reported.                           |
| `outputTokens`     | _number_ | :heavy_minus_sign: | Output token count.                                        |
| `status`           | _string_ | :heavy_minus_sign: | Provider-reported usage or session status, when available. |
