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

| Field               | Type                                                                       | Required           | Description                                                                                                |
| ------------------- | -------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------- |
| `gramKey`           | _string_                                                                   | :heavy_minus_sign: | API Key header                                                                                             |
| `gramProject`       | _string_                                                                   | :heavy_minus_sign: | project header                                                                                             |
| `xGramHookHostname` | _string_                                                                   | :heavy_minus_sign: | Optional endpoint hostname supplied by the Gram hook plugin.                                               |
| `idempotencyKey`    | _string_                                                                   | :heavy_minus_sign: | Optional per-invocation token reused across retries so the server stores a redelivered event exactly once. |
| `codexHookPayload`  | [components.CodexHookPayload](../../models/components/codexhookpayload.md) | :heavy_check_mark: | N/A                                                                                                        |
