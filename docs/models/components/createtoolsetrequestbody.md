# CreateToolsetRequestBody

## Example Usage

```typescript
import { CreateToolsetRequestBody } from "@gram/client/models/components";

let value: CreateToolsetRequestBody = {
  name: "<value>",
};
```

## Fields

| Field                                                             | Type                                                              | Required                                                          | Description                                                       |
| ----------------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------- |
| `defaultEnvironmentSlug`                                          | *string*                                                          | :heavy_minus_sign:                                                | The slug of the environment to use as the default for the toolset |
| `description`                                                     | *string*                                                          | :heavy_minus_sign:                                                | Description of the toolset                                        |
| `name`                                                            | *string*                                                          | :heavy_check_mark:                                                | The name of the toolset                                           |
| `resourceUrns`                                                    | *string*[]                                                        | :heavy_minus_sign:                                                | List of resource URNs to include in the toolset                   |
| `toolUrns`                                                        | *string*[]                                                        | :heavy_minus_sign:                                                | List of tool URNs to include in the toolset                       |