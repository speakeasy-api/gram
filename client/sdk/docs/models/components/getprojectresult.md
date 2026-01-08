# GetProjectResult

## Example Usage

```typescript
import { GetProjectResult } from "@gram/client/models/components";

let value: GetProjectResult = {
  project: {
    createdAt: new Date("2025-09-07T19:46:25.899Z"),
    id: "<id>",
    name: "<value>",
    organizationId: "<id>",
    slug: "<value>",
    updatedAt: new Date("2024-05-13T09:59:59.603Z"),
  },
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `project`                                                | [components.Project](../../models/components/project.md) | :heavy_check_mark:                                       | N/A                                                      |