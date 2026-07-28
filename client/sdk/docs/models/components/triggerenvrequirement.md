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

| Field         | Type      | Required           | Description                       |
| ------------- | --------- | ------------------ | --------------------------------- |
| `description` | _string_  | :heavy_minus_sign: | Description of the variable.      |
| `name`        | _string_  | :heavy_check_mark: | The environment variable name.    |
| `required`    | _boolean_ | :heavy_check_mark: | Whether the variable is required. |
