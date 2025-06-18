# OrganizationEntry

## Example Usage

```typescript
import { OrganizationEntry } from "@gram/client/models/components";

let value: OrganizationEntry = {
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
| `id`                                                                 | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |
| `name`                                                               | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |
| `projects`                                                           | [components.ProjectEntry](../../models/components/projectentry.md)[] | :heavy_check_mark:                                                   | N/A                                                                  |
| `slug`                                                               | *string*                                                             | :heavy_check_mark:                                                   | N/A                                                                  |
| `ssoConnectionId`                                                    | *string*                                                             | :heavy_minus_sign:                                                   | N/A                                                                  |
| `userWorkspaceSlugs`                                                 | *string*[]                                                           | :heavy_minus_sign:                                                   | N/A                                                                  |