# AuthNumberInfoResponse

## Example Usage

```typescript
import { AuthNumberInfoResponse } from "@gram/client/models/operations";

let value: AuthNumberInfoResponse = {
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
        organizationId: "<id>",
        organizationName: "<value>",
        organizationSlug: "<value>",
        projects: [
          {
            projectId: "<id>",
            projectName: "<value>",
            projectSlug: "<value>",
          },
        ],
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