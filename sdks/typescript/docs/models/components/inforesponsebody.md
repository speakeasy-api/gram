# InfoResponseBody

## Example Usage

```typescript
import { InfoResponseBody } from "@gram/sdk/models/components";

let value: InfoResponseBody = {
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
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `activeOrganizationId`                                               | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |
| `organizations`                                                      | [components.Organization](../../models/components/organization.md)[] | :heavy_check_mark:                                                   | N/A                                                                  |
| `userEmail`                                                          | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |
| `userId`                                                             | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |