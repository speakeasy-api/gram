# SessionInfoResponse

## Example Usage

```typescript
import { SessionInfoResponse } from "@gram/client/models/operations";

let value: SessionInfoResponse = {
  headers: {
    "key": [
      "<value>",
    ],
  },
  result: {
    activeOrganizationId: "<id>",
    organizations: [
      {
        accountType: "<value>",
        id: "<id>",
        name: "<value>",
        projects: [
          {
            id: "<id>",
            name: "<value>",
            slug: "<value>",
          },
        ],
        slug: "<value>",
      },
    ],
    userEmail: "<value>",
    userId: "<id>",
  },
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `headers`                                                                  | Record<string, *string*[]>                                                 | :heavy_check_mark:                                                         | N/A                                                                        |
| `result`                                                                   | [components.InfoResponseBody](../../models/components/inforesponsebody.md) | :heavy_check_mark:                                                         | N/A                                                                        |