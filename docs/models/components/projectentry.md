# ProjectEntry

## Example Usage

```typescript
import { ProjectEntry } from "@gram/client/models/components";

let value: ProjectEntry = {
  id: "<id>",
  name: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `id`                                                            | *string*                                                        | :heavy_check_mark:                                              | The ID of the project                                           |
| `name`                                                          | *string*                                                        | :heavy_check_mark:                                              | The name of the project                                         |
| `slug`                                                          | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |