# HookToolCallData

Tool call feature payload.

## Example Usage

```typescript
import { HookToolCallData } from "@gram/client/models/components/hooktoolcalldata.js";

let value: HookToolCallData = {};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `durationMs`                                             | *number*                                                 | :heavy_minus_sign:                                       | Tool execution duration in milliseconds, when reported.  |
| `error`                                                  | *any*                                                    | :heavy_minus_sign:                                       | Tool error payload.                                      |
| `id`                                                     | *string*                                                 | :heavy_minus_sign:                                       | Provider tool call identifier.                           |
| `input`                                                  | *any*                                                    | :heavy_minus_sign:                                       | Tool input payload.                                      |
| `isInterrupt`                                            | *boolean*                                                | :heavy_minus_sign:                                       | Whether the failure was caused by user interruption.     |
| `name`                                                   | *string*                                                 | :heavy_minus_sign:                                       | Tool name.                                               |
| `output`                                                 | *any*                                                    | :heavy_minus_sign:                                       | Tool output payload.                                     |
| `permissionType`                                         | *string*                                                 | :heavy_minus_sign:                                       | Permission type requested by the agent, when applicable. |
| `status`                                                 | *string*                                                 | :heavy_minus_sign:                                       | Provider-reported tool call status, when available.      |