# SearchToolCallsRequest

## Example Usage

```typescript
import { SearchToolCallsRequest } from "@gram/client/models/operations/searchtoolcalls.js";

let value: SearchToolCallsRequest = {
  searchToolCallsPayload: {
    filter: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
  },
};
```

## Fields

| Field                    | Type                                                                                   | Required           | Description    |
| ------------------------ | -------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                | _string_                                                                               | :heavy_minus_sign: | API Key header |
| `gramSession`            | _string_                                                                               | :heavy_minus_sign: | Session header |
| `gramProject`            | _string_                                                                               | :heavy_minus_sign: | project header |
| `searchToolCallsPayload` | [components.SearchToolCallsPayload](../../models/components/searchtoolcallspayload.md) | :heavy_check_mark: | N/A            |
