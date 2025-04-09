# UpdateToolsetRequestBody

## Example Usage

```typescript
import { UpdateToolsetRequestBody } from "@gram/sdk/models/components";

let value: UpdateToolsetRequestBody = {};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `defaultEnvironmentId`                                          | *string*                                                        | :heavy_minus_sign:                                              | The ID of the environment to use as the default for the toolset |
| `description`                                                   | *string*                                                        | :heavy_minus_sign:                                              | The new description of the toolset                              |
| `httpToolNames`                                                 | *string*[]                                                      | :heavy_minus_sign:                                              | List of HTTP tool names to include                              |
| `name`                                                          | *string*                                                        | :heavy_minus_sign:                                              | The new name of the toolset                                     |