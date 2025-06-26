# InfoResponseBody

## Example Usage

```typescript
import { InfoResponseBody } from "@gram/client/models/components";

let value: InfoResponseBody = {
  activeOrganizationId: "<id>",
  gramAccountType: "<value>",
  isAdmin: false,
  organizations: [],
  userEmail: "<value>",
  userId: "<id>",
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `activeOrganizationId`                                                         | *string*                                                                       | :heavy_check_mark:                                                             | N/A                                                                            |
| `gramAccountType`                                                              | *string*                                                                       | :heavy_check_mark:                                                             | N/A                                                                            |
| `isAdmin`                                                                      | *boolean*                                                                      | :heavy_check_mark:                                                             | N/A                                                                            |
| `organizations`                                                                | [components.OrganizationEntry](../../models/components/organizationentry.md)[] | :heavy_check_mark:                                                             | N/A                                                                            |
| `userDisplayName`                                                              | *string*                                                                       | :heavy_minus_sign:                                                             | N/A                                                                            |
| `userEmail`                                                                    | *string*                                                                       | :heavy_check_mark:                                                             | N/A                                                                            |
| `userId`                                                                       | *string*                                                                       | :heavy_check_mark:                                                             | N/A                                                                            |
| `userPhotoUrl`                                                                 | *string*                                                                       | :heavy_minus_sign:                                                             | N/A                                                                            |
| `userSignature`                                                                | *string*                                                                       | :heavy_minus_sign:                                                             | N/A                                                                            |