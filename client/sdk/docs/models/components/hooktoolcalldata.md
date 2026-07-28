# HookToolCallData

Tool call feature payload.

## Example Usage

```typescript
import { HookToolCallData } from "@gram/client/models/components/hooktoolcalldata.js";

let value: HookToolCallData = {};
```

## Fields

| Field            | Type      | Required           | Description                                              |
| ---------------- | --------- | ------------------ | -------------------------------------------------------- |
| `durationMs`     | _number_  | :heavy_minus_sign: | Tool execution duration in milliseconds, when reported.  |
| `error`          | _any_     | :heavy_minus_sign: | Tool error payload.                                      |
| `id`             | _string_  | :heavy_minus_sign: | Provider tool call identifier.                           |
| `input`          | _any_     | :heavy_minus_sign: | Tool input payload.                                      |
| `isInterrupt`    | _boolean_ | :heavy_minus_sign: | Whether the failure was caused by user interruption.     |
| `name`           | _string_  | :heavy_minus_sign: | Tool name.                                               |
| `output`         | _any_     | :heavy_minus_sign: | Tool output payload.                                     |
| `permissionType` | _string_  | :heavy_minus_sign: | Permission type requested by the agent, when applicable. |
| `status`         | _string_  | :heavy_minus_sign: | Provider-reported tool call status, when available.      |
