# InfoResponseBody

## Example Usage

```typescript
import { InfoResponseBody } from "@gram/client/models/components/inforesponsebody.js";

let value: InfoResponseBody = {
  activeOrganizationId: "<id>",
  gramAccountType: "<value>",
  hasActiveSubscription: false,
  isAdmin: true,
  organizations: [],
  userEmail: "<value>",
  userId: "<id>",
  whitelisted: true,
};
```

## Fields

| Field                   | Type                                                                           | Required           | Description                                                    |
| ----------------------- | ------------------------------------------------------------------------------ | ------------------ | -------------------------------------------------------------- |
| `activeOrganizationId`  | _string_                                                                       | :heavy_check_mark: | N/A                                                            |
| `gramAccountType`       | _string_                                                                       | :heavy_check_mark: | N/A                                                            |
| `hasActiveSubscription` | _boolean_                                                                      | :heavy_check_mark: | Whether the organization has an active billing subscription    |
| `isAdmin`               | _boolean_                                                                      | :heavy_check_mark: | N/A                                                            |
| `organizations`         | [components.OrganizationEntry](../../models/components/organizationentry.md)[] | :heavy_check_mark: | N/A                                                            |
| `userDisplayName`       | _string_                                                                       | :heavy_minus_sign: | N/A                                                            |
| `userEmail`             | _string_                                                                       | :heavy_check_mark: | N/A                                                            |
| `userId`                | _string_                                                                       | :heavy_check_mark: | N/A                                                            |
| `userPhotoUrl`          | _string_                                                                       | :heavy_minus_sign: | N/A                                                            |
| `userSignature`         | _string_                                                                       | :heavy_minus_sign: | N/A                                                            |
| `whitelisted`           | _boolean_                                                                      | :heavy_check_mark: | Whether the organization is whitelisted to access the platform |
