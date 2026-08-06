# ListAllOrganizationsResult

## Example Usage

```typescript
import { ListAllOrganizationsResult } from "@gram/client/models/components";

let value: ListAllOrganizationsResult = {
  limit: 152767,
  offset: 798331,
  organizations: [
    {
      createdAt: new Date("2026-04-29T22:25:18.776Z"),
      gramAccountType: "enterprise",
      id: "<id>",
      name: "<value>",
      slug: "<value>",
      updatedAt: new Date("2024-09-13T18:55:20.631Z"),
      whitelisted: true,
    },
  ],
  total: 388823,
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `limit`                                                                            | *number*                                                                           | :heavy_check_mark:                                                                 | Maximum number of organizations returned in this response.                         |
| `offset`                                                                           | *number*                                                                           | :heavy_check_mark:                                                                 | Number of organizations skipped before this page.                                  |
| `organizations`                                                                    | [components.OrganizationSummary](../../models/components/organizationsummary.md)[] | :heavy_check_mark:                                                                 | Gram organizations for this page.                                                  |
| `total`                                                                            | *number*                                                                           | :heavy_check_mark:                                                                 | Total number of Gram organizations (ignores limit/offset).                         |