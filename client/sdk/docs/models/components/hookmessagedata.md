# HookMessageData

Assistant/user message payload.

## Example Usage

```typescript
import { HookMessageData } from "@gram/client/models/components/hookmessagedata.js";

let value: HookMessageData = {};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `durationMs`                                                       | *number*                                                           | :heavy_minus_sign:                                                 | Message or thinking-block duration in milliseconds, when reported. |
| `role`                                                             | *string*                                                           | :heavy_minus_sign:                                                 | Message role, e.g. assistant or user.                              |
| `text`                                                             | *string*                                                           | :heavy_minus_sign:                                                 | Message text.                                                      |