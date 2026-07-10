# IngestRequestBody

## Example Usage

```typescript
import { IngestRequestBody } from "@gram/client/models/components/ingestrequestbody.js";

let value: IngestRequestBody = {
  event: {
    type: "session.ended",
  },
  schemaVersion: "<value>",
  source: {
    adapter: "<value>",
  },
};
```

## Fields

| Field                                                                                               | Type                                                                                                | Required                                                                                            | Description                                                                                         |
| --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `data`                                                                                              | [components.HookIngestData](../../models/components/hookingestdata.md)                              | :heavy_minus_sign:                                                                                  | Feature-specific payloads. Hooks populate only the blocks needed for the event.                     |
| `event`                                                                                             | [components.HookIngestEvent](../../models/components/hookingestevent.md)                            | :heavy_check_mark:                                                                                  | Canonical Gram feature event.                                                                       |
| `raw`                                                                                               | *any*                                                                                               | :heavy_minus_sign:                                                                                  | Original provider payload for debugging. The backend does not use this for feature behavior.        |
| `schemaVersion`                                                                                     | *string*                                                                                            | :heavy_check_mark:                                                                                  | Contract version. The current version is hook.ingest.v1.                                            |
| `session`                                                                                           | [components.HookIngestSession](../../models/components/hookingestsession.md)                        | :heavy_minus_sign:                                                                                  | Agent session and turn identity, independent of provider naming.                                    |
| `source`                                                                                            | [components.HookIngestSource](../../models/components/hookingestsource.md)                          | :heavy_check_mark:                                                                                  | Metadata about the local hook adapter that translated a provider event into the Gram hook contract. |