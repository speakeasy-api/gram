# EnvironmentsNumberDeleteEnvironmentRequest

## Example Usage

```typescript
import { EnvironmentsNumberDeleteEnvironmentRequest } from "@gram/sdk/models/operations";

let value: EnvironmentsNumberDeleteEnvironmentRequest = {
  slug: "<value>",
};
```

## Fields

| Field                                 | Type                                  | Required                              | Description                           |
| ------------------------------------- | ------------------------------------- | ------------------------------------- | ------------------------------------- |
| `slug`                                | *string*                              | :heavy_check_mark:                    | The slug of the environment to delete |
| `gramSession`                         | *string*                              | :heavy_minus_sign:                    | Session header                        |
| `gramProject`                         | *string*                              | :heavy_minus_sign:                    | project header                        |