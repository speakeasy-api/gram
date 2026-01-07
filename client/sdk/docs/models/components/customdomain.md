# CustomDomain

## Example Usage

```typescript
import { CustomDomain } from "@gram/client/models/components";

let value: CustomDomain = {
  activated: false,
  createdAt: new Date("2026-10-29T10:18:42.496Z"),
  domain: "glass-giggle.name",
  id: "<id>",
  isUpdating: false,
  organizationId: "<id>",
  updatedAt: new Date("2024-06-21T11:19:48.376Z"),
  verified: false,
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `activated`                                                                                   | *boolean*                                                                                     | :heavy_check_mark:                                                                            | Whether the domain is activated in ingress                                                    |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the custom domain was created.                                                           |
| `domain`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The custom domain name                                                                        |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the custom domain                                                                   |
| `isUpdating`                                                                                  | *boolean*                                                                                     | :heavy_check_mark:                                                                            | The custom domain is actively being registered                                                |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the organization this domain belongs to                                             |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the custom domain was last updated.                                                      |
| `verified`                                                                                    | *boolean*                                                                                     | :heavy_check_mark:                                                                            | Whether the domain is verified                                                                |