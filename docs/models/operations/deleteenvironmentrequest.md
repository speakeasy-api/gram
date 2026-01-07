# DeleteEnvironmentRequest

## Example Usage

```typescript
import { DeleteEnvironmentRequest } from "@gram/client/models/operations";

let value: DeleteEnvironmentRequest = {
  slug: "<value>",
};
```

## Fields

| Field                                 | Type                                  | Required                              | Description                           |
| ------------------------------------- | ------------------------------------- | ------------------------------------- | ------------------------------------- |
| `slug`                                | *string*                              | :heavy_check_mark:                    | The slug of the environment to delete |
| `gramSession`                         | *string*                              | :heavy_minus_sign:                    | Session header                        |
| `gramProject`                         | *string*                              | :heavy_minus_sign:                    | project header                        |