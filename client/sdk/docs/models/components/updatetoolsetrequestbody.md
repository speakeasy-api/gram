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
| `httpToolNamesToAdd`                                            | *string*[]                                                      | :heavy_minus_sign:                                              | HTTP tool names to add to the toolset                           |
| `httpToolNamesToRemove`                                         | *string*[]                                                      | :heavy_minus_sign:                                              | HTTP tool names to remove from the toolset                      |
| `name`                                                          | *string*                                                        | :heavy_minus_sign:                                              | The new name of the toolset                                     |