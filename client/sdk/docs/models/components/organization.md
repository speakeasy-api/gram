# Organization

## Example Usage

```typescript
import { Organization } from "@gram/sdk/models/components";

let value: Organization = {
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
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `accountType`                                              | *string*                                                   | :heavy_check_mark:                                         | N/A                                                        |
| `organizationId`                                           | *string*                                                   | :heavy_check_mark:                                         | N/A                                                        |
| `organizationName`                                         | *string*                                                   | :heavy_check_mark:                                         | N/A                                                        |
| `organizationSlug`                                         | *string*                                                   | :heavy_check_mark:                                         | N/A                                                        |
| `projects`                                                 | [components.Project](../../models/components/project.md)[] | :heavy_check_mark:                                         | N/A                                                        |