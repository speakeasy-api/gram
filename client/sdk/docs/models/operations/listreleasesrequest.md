# ListReleasesRequest

## Example Usage

```typescript
import { ListReleasesRequest } from "@gram/client/models/operations";

let value: ListReleasesRequest = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                | Type                                 | Required                             | Description                          |
| ------------------------------------ | ------------------------------------ | ------------------------------------ | ------------------------------------ |
| `toolsetSlug`                        | *string*                             | :heavy_check_mark:                   | The slug of the toolset              |
| `limit`                              | *number*                             | :heavy_minus_sign:                   | Maximum number of releases to return |
| `offset`                             | *number*                             | :heavy_minus_sign:                   | Offset for pagination                |
| `gramSession`                        | *string*                             | :heavy_minus_sign:                   | Session header                       |
| `gramKey`                            | *string*                             | :heavy_minus_sign:                   | API Key header                       |
| `gramProject`                        | *string*                             | :heavy_minus_sign:                   | project header                       |