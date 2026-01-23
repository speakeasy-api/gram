# SessionInfoResponse

## Example Usage

```typescript
import { SessionInfoResponse } from "@gram/client/models/operations";

let value: SessionInfoResponse = {
  headers: {
    "key": [],
    "key1": [
      "<value 1>",
      "<value 2>",
    ],
  },
  result: {
    activeOrganizationId: "<id>",
    gramAccountType: "<value>",
    isAdmin: true,
    organizations: [
      {
        id: "<id>",
        name: "<value>",
        projects: [],
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