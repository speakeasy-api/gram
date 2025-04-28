# InfoResponseBody

## Example Usage

```typescript
import { InfoResponseBody } from "@gram/client/models/components";

let value: InfoResponseBody = {
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
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `activeOrganizationId`                                                         | *string*                                                                       | :heavy_check_mark:                                                             | N/A                                                                            |
| `organizations`                                                                | [components.OrganizationEntry](../../models/components/organizationentry.md)[] | :heavy_check_mark:                                                             | N/A                                                                            |
| `userEmail`                                                                    | *string*                                                                       | :heavy_check_mark:                                                             | N/A                                                                            |
| `userId`                                                                       | *string*                                                                       | :heavy_check_mark:                                                             | N/A                                                                            |