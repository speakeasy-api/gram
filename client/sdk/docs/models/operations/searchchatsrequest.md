# SearchChatsRequest

## Example Usage

```typescript
import { SearchChatsRequest } from "@gram/client/models/operations/searchchats.js";

let value: SearchChatsRequest = {
  searchChatsPayload: {
    filter: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  },
};
```

## Fields

| Field                | Type                                                                           | Required           | Description    |
| -------------------- | ------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`            | _string_                                                                       | :heavy_minus_sign: | API Key header |
| `gramSession`        | _string_                                                                       | :heavy_minus_sign: | Session header |
| `gramProject`        | _string_                                                                       | :heavy_minus_sign: | project header |
| `searchChatsPayload` | [components.SearchChatsPayload](../../models/components/searchchatspayload.md) | :heavy_check_mark: | N/A            |
