# HooksNumberClaudeRequest

## Example Usage

```typescript
import { HooksNumberClaudeRequest } from "@gram/client/models/operations/hooksnumberclaude.js";

let value: HooksNumberClaudeRequest = {
  claudeHookPayload: {
    hookEventName: "ConfigChange",
  },
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                  | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Optional API key for plugin-driven attribution.                                                            |
| `gramProject`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Optional project slug for plugin-driven attribution.                                                       |
| `xGramHookHostname`                                                                                        | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Optional endpoint hostname supplied by the Gram hook plugin.                                               |
| `idempotencyKey`                                                                                           | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Optional per-invocation token reused across retries so the server stores a redelivered event exactly once. |
| `claudeHookPayload`                                                                                        | [components.ClaudeHookPayload](../../models/components/claudehookpayload.md)                               | :heavy_check_mark:                                                                                         | N/A                                                                                                        |