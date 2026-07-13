# IngestHookResult

Provider-neutral decision returned by the unified hook endpoint.

## Example Usage

```typescript
import { IngestHookResult } from "@gram/client/models/components/ingesthookresult.js";

let value: IngestHookResult = {
  decision: "allow",
};
```

## Fields

| Field      | Type                                                       | Required           | Description                                             |
| ---------- | ---------------------------------------------------------- | ------------------ | ------------------------------------------------------- |
| `decision` | [components.Decision](../../models/components/decision.md) | :heavy_check_mark: | Whether the local hook should allow or deny the action. |
| `effects`  | Record<string, _any_>                                      | :heavy_minus_sign: | Optional side-effect hints for hook SDKs.               |
| `message`  | _string_                                                   | :heavy_minus_sign: | User-facing decision message.                           |
| `reason`   | _string_                                                   | :heavy_minus_sign: | Machine-readable decision reason.                       |
