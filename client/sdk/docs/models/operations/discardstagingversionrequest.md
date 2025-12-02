# DiscardStagingVersionRequest

## Example Usage

```typescript
import { DiscardStagingVersionRequest } from "@gram/client/models/operations";

let value: DiscardStagingVersionRequest = {
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