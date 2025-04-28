# OrganizationEntry

## Example Usage

```typescript
import { OrganizationEntry } from "@gram/client/models/components";

let value: OrganizationEntry = {
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
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `accountType`                                                        | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |
| `id`                                                                 | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |
| `name`                                                               | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |
| `projects`                                                           | [components.ProjectEntry](../../models/components/projectentry.md)[] | :heavy_check_mark:                                                   | N/A                                                                  |
| `slug`                                                               | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |