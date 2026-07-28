# FunctionEnvironmentVariable

## Example Usage

```typescript
import { FunctionEnvironmentVariable } from "@gram/client/models/components/functionenvironmentvariable.js";

let value: FunctionEnvironmentVariable = {
  name: "<value>",
};
```

## Fields

| Field           | Type     | Required           | Description                                                              |
| --------------- | -------- | ------------------ | ------------------------------------------------------------------------ |
| `authInputType` | _string_ | :heavy_minus_sign: | Optional value of the function variable comes from a specific auth input |
| `description`   | _string_ | :heavy_minus_sign: | Description of the function environment variable                         |
| `name`          | _string_ | :heavy_check_mark: | The environment variables                                                |
