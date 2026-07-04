# HookUsageData

Token and cost usage payload.

## Fields

| Field              | Type       | Required           | Description                                                |
| ------------------ | ---------- | ------------------ | ---------------------------------------------------------- |
| `CacheReadTokens`  | `*int64`   | :heavy_minus_sign: | Cache read token count.                                    |
| `CacheWriteTokens` | `*int64`   | :heavy_minus_sign: | Cache write token count.                                   |
| `Cost`             | `*float64` | :heavy_minus_sign: | Reported cost.                                             |
| `InputTokens`      | `*int64`   | :heavy_minus_sign: | Input token count.                                         |
| `LoopCount`        | `*int64`   | :heavy_minus_sign: | Agent loop count, when reported.                           |
| `OutputTokens`     | `*int64`   | :heavy_minus_sign: | Output token count.                                        |
| `Status`           | `*string`  | :heavy_minus_sign: | Provider-reported usage or session status, when available. |
