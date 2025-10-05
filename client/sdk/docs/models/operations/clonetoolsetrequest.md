# CloneToolsetRequest

## Example Usage

```typescript
import { CloneToolsetRequest } from "@gram/client/models/operations";

let value: CloneToolsetRequest = {
  slug: "<value>",
};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `slug`                           | *string*                         | :heavy_check_mark:               | The slug of the toolset to clone |
| `gramSession`                    | *string*                         | :heavy_minus_sign:               | Session header                   |
| `gramProject`                    | *string*                         | :heavy_minus_sign:               | project header                   |