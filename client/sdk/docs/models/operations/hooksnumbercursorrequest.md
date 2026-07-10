# HooksNumberCursorRequest

## Example Usage

```typescript
import { HooksNumberCursorRequest } from "@gram/client/models/operations/hooksnumbercursor.js";

let value: HooksNumberCursorRequest = {
  cursorHookPayload: {
    hookEventName: "<value>",
  },
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                  | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | API Key header                                                                                             |
| `gramProject`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | project header                                                                                             |
| `xGramHookHostname`                                                                                        | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Optional endpoint hostname supplied by the Gram hook plugin.                                               |
| `idempotencyKey`                                                                                           | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Optional per-invocation token reused across retries so the server stores a redelivered event exactly once. |
| `cursorHookPayload`                                                                                        | [components.CursorHookPayload](../../models/components/cursorhookpayload.md)                               | :heavy_check_mark:                                                                                         | N/A                                                                                                        |