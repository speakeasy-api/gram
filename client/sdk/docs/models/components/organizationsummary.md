# OrganizationSummary

## Example Usage

```typescript
import { OrganizationSummary } from "@gram/client/models/components";

let value: OrganizationSummary = {
  createdAt: new Date("2026-03-24T04:14:40.349Z"),
  gramAccountType: "free",
  id: "<id>",
  name: "<value>",
  slug: "<value>",
  updatedAt: new Date("2025-07-08T03:46:09.999Z"),
  whitelisted: true,
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `disabledAt`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | When the organization was disabled, if applicable.                                            |
| `gramAccountType`                                                                             | [components.GramAccountType](../../models/components/gramaccounttype.md)                      | :heavy_check_mark:                                                                            | Gram account tier.                                                                            |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Gram organization ID.                                                                         |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Organization display name.                                                                    |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Organization slug.                                                                            |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `whitelisted`                                                                                 | *boolean*                                                                                     | :heavy_check_mark:                                                                            | Whether the organization is whitelisted.                                                      |
| `workosId`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | WorkOS organization ID, when linked.                                                          |