# ListUserSessionConsentsResponse

## Example Usage

```typescript
import { ListUserSessionConsentsResponse } from "@gram/client/models/operations/listusersessionconsents.js";

let value: ListUserSessionConsentsResponse = {
  result: {
    items: [
      {
        consentedAt: new Date("2026-05-06T12:21:15.259Z"),
        createdAt: new Date("2026-03-05T02:30:34.466Z"),
        id: "3f3f3776-540b-4ec9-9808-8b049adbf1c6",
        remoteSetHash: "<value>",
        subjectUrn: "<value>",
        updatedAt: new Date("2026-06-21T06:40:28.157Z"),
        userSessionClientId: "8c08a879-7b85-40c1-9ada-c99aa7d3c9ce",
      },
    ],
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `result`                                                                                             | [components.ListUserSessionConsentsResult](../../models/components/listusersessionconsentsresult.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |