# Project

## Example Usage

```typescript
import { Project } from "@gram/client/models/components";

let value: Project = {
  projectId: "<id>",
  projectName: "<value>",
  projectSlug: "<value>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `projectId`                                                     | *string*                                                        | :heavy_check_mark:                                              | N/A                                                             |
| `projectName`                                                   | *string*                                                        | :heavy_check_mark:                                              | N/A                                                             |
| `projectSlug`                                                   | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |