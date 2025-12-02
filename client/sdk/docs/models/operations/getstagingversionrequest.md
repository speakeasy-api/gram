# GetStagingVersionRequest

## Example Usage

```typescript
import { GetStagingVersionRequest } from "@gram/client/models/operations";

let value: GetStagingVersionRequest = {
  slug: "<value>",
};
```

## Fields

| Field                          | Type                           | Required                       | Description                    |
| ------------------------------ | ------------------------------ | ------------------------------ | ------------------------------ |
| `slug`                         | *string*                       | :heavy_check_mark:             | The slug of the parent toolset |
| `gramSession`                  | *string*                       | :heavy_minus_sign:             | Session header                 |
| `gramKey`                      | *string*                       | :heavy_minus_sign:             | API Key header                 |
| `gramProject`                  | *string*                       | :heavy_minus_sign:             | project header                 |