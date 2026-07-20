# CustomDomain

## Example Usage

```typescript
import { CustomDomain } from "@gram/client/models/components/customdomain.js";

let value: CustomDomain = {
  activated: false,
  createdAt: new Date("2026-10-29T10:18:42.496Z"),
  domain: "glass-giggle.name",
  id: "<id>",
  ipAllowlist: ["<value 1>", "<value 2>"],
  isUpdating: true,
  organizationId: "<id>",
  updatedAt: new Date("2026-02-14T10:38:05.244Z"),
  verified: true,
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                                                               |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------------------- |
| `activated`      | _boolean_                                                                                     | :heavy_check_mark: | Whether the domain is activated in ingress                                                |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the custom domain was created.                                                       |
| `domain`         | _string_                                                                                      | :heavy_check_mark: | The custom domain name                                                                    |
| `id`             | _string_                                                                                      | :heavy_check_mark: | The ID of the custom domain                                                               |
| `ipAllowlist`    | _string_[]                                                                                    | :heavy_check_mark: | IP addresses or CIDR ranges allowed to access this domain. Empty list means unrestricted. |
| `isUpdating`     | _boolean_                                                                                     | :heavy_check_mark: | The custom domain is actively being registered                                            |
| `organizationId` | _string_                                                                                      | :heavy_check_mark: | The ID of the organization this domain belongs to                                         |
| `updatedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the custom domain was last updated.                                                  |
| `verified`       | _boolean_                                                                                     | :heavy_check_mark: | Whether the domain is verified                                                            |
