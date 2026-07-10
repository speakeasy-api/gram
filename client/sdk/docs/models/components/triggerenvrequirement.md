# TriggerEnvRequirement

## Example Usage

```typescript
import { TriggerEnvRequirement } from "@gram/client/models/components/triggerenvrequirement.js";

let value: TriggerEnvRequirement = {
  name: "<value>",
  required: true,
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `description`                     | *string*                          | :heavy_minus_sign:                | Description of the variable.      |
| `name`                            | *string*                          | :heavy_check_mark:                | The environment variable name.    |
| `required`                        | *boolean*                         | :heavy_check_mark:                | Whether the variable is required. |