# HooksNumberCodexRequest

## Example Usage

```typescript
import { HooksNumberCodexRequest } from "@gram/client/models/operations/hooksnumbercodex.js";

let value: HooksNumberCodexRequest = {
  codexHookPayload: {
    hookEventName: "PermissionRequest",
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
| `codexHookPayload`                                                                                         | [components.CodexHookPayload](../../models/components/codexhookpayload.md)                                 | :heavy_check_mark:                                                                                         | N/A                                                                                                        |