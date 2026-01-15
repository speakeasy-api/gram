# PublishToolsetRequest

## Example Usage

```typescript
import { PublishToolsetRequest } from "@gram/client/models/operations";

let value: PublishToolsetRequest = {
  slug: "<value>",
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `slug`                             | *string*                           | :heavy_check_mark:                 | The slug of the toolset to publish |
| `gramSession`                      | *string*                           | :heavy_minus_sign:                 | Session header                     |
| `gramKey`                          | *string*                           | :heavy_minus_sign:                 | API Key header                     |
| `gramProject`                      | *string*                           | :heavy_minus_sign:                 | project header                     |