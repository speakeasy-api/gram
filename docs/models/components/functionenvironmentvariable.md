# FunctionEnvironmentVariable

## Example Usage

```typescript
import { FunctionEnvironmentVariable } from "@gram/client/models/components";

let value: FunctionEnvironmentVariable = {
  name: "<value>",
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `authInputType`                                                          | *string*                                                                 | :heavy_minus_sign:                                                       | Optional value of the function variable comes from a specific auth input |
| `description`                                                            | *string*                                                                 | :heavy_minus_sign:                                                       | Description of the function environment variable                         |
| `name`                                                                   | *string*                                                                 | :heavy_check_mark:                                                       | The environment variables                                                |