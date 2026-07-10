# ListUserSessionIssuersResponse

## Example Usage

```typescript
import { ListUserSessionIssuersResponse } from "@gram/client/models/operations/listusersessionissuers.js";

let value: ListUserSessionIssuersResponse = {
  result: {
    items: [
      {
        authnChallengeMode: "<value>",
        createdAt: new Date("2025-12-23T14:08:20.307Z"),
        id: "64feecfb-e588-48ca-85a1-c9174693d771",
        projectId: "01a5eb3d-85c6-4a71-b663-32fe2ea6f7d2",
        sessionDurationHours: 210087,
        slug: "<value>",
        updatedAt: new Date("2024-10-28T12:21:52.111Z"),
      },
    ],
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `result`                                                                                           | [components.ListUserSessionIssuersResult](../../models/components/listusersessionissuersresult.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |