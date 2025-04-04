# CreateToolsetRequestBody

## Example Usage

```typescript
import { CreateToolsetRequestBody } from "@gram/sdk/models/components";

let value: CreateToolsetRequestBody = {
  name: "<value>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `defaultEnvironmentId`                                          | *string*                                                        | :heavy_minus_sign:                                              | The ID of the environment to use as the default for the toolset |
| `description`                                                   | *string*                                                        | :heavy_minus_sign:                                              | Description of the toolset                                      |
| `httpToolIds`                                                   | *string*[]                                                      | :heavy_minus_sign:                                              | List of HTTP tool IDs to include                                |
| `name`                                                          | *string*                                                        | :heavy_check_mark:                                              | The name of the toolset                                         |