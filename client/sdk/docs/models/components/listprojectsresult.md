# ListProjectsResult

## Example Usage

```typescript
import { ListProjectsResult } from "@gram/client/models/components";

let value: ListProjectsResult = {
  projects: [
    {
      id: "<id>",
      name: "<value>",
      slug: "<value>",
    },
  ],
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `projects`                                                           | [components.ProjectEntry](../../models/components/projectentry.md)[] | :heavy_check_mark:                                                   | The list of projects                                                 |