# CreateEnvironmentForm

Form for creating a new environment

## Example Usage

```typescript
import { CreateEnvironmentForm } from "@gram/client/models/components/createenvironmentform.js";

let value: CreateEnvironmentForm = {
  entries: [
    {
      name: "<value>",
      value: "<value>",
    },
  ],
  name: "<value>",
  organizationId: "<id>",
};
```

## Fields

| Field            | Type                                                                                   | Required           | Description                                     |
| ---------------- | -------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------- |
| `description`    | _string_                                                                               | :heavy_minus_sign: | Optional description of the environment         |
| `entries`        | [components.EnvironmentEntryInput](../../models/components/environmententryinput.md)[] | :heavy_check_mark: | List of environment variable entries            |
| `name`           | _string_                                                                               | :heavy_check_mark: | The name of the environment                     |
| `organizationId` | _string_                                                                               | :heavy_check_mark: | The organization ID this environment belongs to |
