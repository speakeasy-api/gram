# GetProjectRequest

## Example Usage

```typescript
import { GetProjectRequest } from "@gram/client/models/operations";

let value: GetProjectRequest = {
  slug: "<value>",
};
```

## Fields

| Field                          | Type                           | Required                       | Description                    |
| ------------------------------ | ------------------------------ | ------------------------------ | ------------------------------ |
| `slug`                         | *string*                       | :heavy_check_mark:             | The slug of the project to get |
| `gramKey`                      | *string*                       | :heavy_minus_sign:             | API Key header                 |
| `gramSession`                  | *string*                       | :heavy_minus_sign:             | Session header                 |