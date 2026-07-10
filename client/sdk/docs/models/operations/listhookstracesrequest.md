# ListHooksTracesRequest

## Example Usage

```typescript
import { ListHooksTracesRequest } from "@gram/client/models/operations/listhookstraces.js";

let value: ListHooksTracesRequest = {
  listHooksTracesPayload: {
    filters: [
      {
        path: "@user.region",
      },
    ],
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
    typesToInclude: [
      "mcp",
      "skill",
    ],
  },
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `gramKey`                                                                              | *string*                                                                               | :heavy_minus_sign:                                                                     | API Key header                                                                         |
| `gramSession`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | Session header                                                                         |
| `gramProject`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | project header                                                                         |
| `listHooksTracesPayload`                                                               | [components.ListHooksTracesPayload](../../models/components/listhookstracespayload.md) | :heavy_check_mark:                                                                     | N/A                                                                                    |