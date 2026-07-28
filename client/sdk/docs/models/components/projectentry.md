# ProjectEntry

## Example Usage

```typescript
import { ProjectEntry } from "@gram/client/models/components/projectentry.js";

let value: ProjectEntry = {
  id: "<id>",
  name: "<value>",
  slug: "<value>",
};
```

## Fields

| Field  | Type     | Required           | Description                                                     |
| ------ | -------- | ------------------ | --------------------------------------------------------------- |
| `id`   | _string_ | :heavy_check_mark: | The ID of the project                                           |
| `name` | _string_ | :heavy_check_mark: | The name of the project                                         |
| `slug` | _string_ | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource. |
