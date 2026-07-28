# IngestHookEventRequest

## Example Usage

```typescript
import { IngestHookEventRequest } from "@gram/client/models/operations/ingesthookevent.js";

let value: IngestHookEventRequest = {
  ingestRequestBody: {
    event: {
      type: "session.ended",
    },
    schemaVersion: "<value>",
    source: {
      adapter: "<value>",
    },
  },
};
```

## Fields

| Field               | Type                                                                         | Required           | Description                                                                                                |
| ------------------- | ---------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------- |
| `gramKey`           | _string_                                                                     | :heavy_minus_sign: | API Key header                                                                                             |
| `gramProject`       | _string_                                                                     | :heavy_minus_sign: | project header                                                                                             |
| `idempotencyKey`    | _string_                                                                     | :heavy_minus_sign: | Optional per-invocation token reused across retries so the server stores a redelivered event exactly once. |
| `ingestRequestBody` | [components.IngestRequestBody](../../models/components/ingestrequestbody.md) | :heavy_check_mark: | N/A                                                                                                        |
