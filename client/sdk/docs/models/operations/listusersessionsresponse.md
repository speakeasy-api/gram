# ListUserSessionsResponse

## Example Usage

```typescript
import { ListUserSessionsResponse } from "@gram/client/models/operations/listusersessions.js";

let value: ListUserSessionsResponse = {
  result: {
    items: [
      {
        createdAt: new Date("2025-03-23T05:23:03.362Z"),
        expiresAt: new Date("2025-03-02T00:32:28.085Z"),
        id: "8ba9d13a-2bd6-473e-a518-97e6eb288e50",
        issuerSlug: "<value>",
        jti: "<value>",
        refreshExpiresAt: new Date("2025-02-21T04:56:07.500Z"),
        subjectType: "<value>",
        subjectUrn: "<value>",
        updatedAt: new Date("2026-07-30T04:55:18.446Z"),
        userSessionIssuerId: "840b67aa-0a5b-422c-bc59-0856ecf8f06a",
      },
    ],
  },
};
```

## Fields

| Field    | Type                                                                                   | Required           | Description |
| -------- | -------------------------------------------------------------------------------------- | ------------------ | ----------- |
| `result` | [components.ListUserSessionsResult](../../models/components/listusersessionsresult.md) | :heavy_check_mark: | N/A         |
