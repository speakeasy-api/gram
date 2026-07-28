# SearchLogsRequest

## Example Usage

```typescript
import { SearchLogsRequest } from "@gram/client/models/operations/searchlogs.js";

let value: SearchLogsRequest = {
  searchLogsPayload: {
    filter: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
    filters: [
      {
        path: "@user.region",
      },
    ],
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
  },
};
```

## Fields

| Field               | Type                                                                         | Required           | Description    |
| ------------------- | ---------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`           | _string_                                                                     | :heavy_minus_sign: | API Key header |
| `gramSession`       | _string_                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`       | _string_                                                                     | :heavy_minus_sign: | project header |
| `searchLogsPayload` | [components.SearchLogsPayload](../../models/components/searchlogspayload.md) | :heavy_check_mark: | N/A            |
