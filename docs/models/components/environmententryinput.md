# EnvironmentEntryInput

A single environment entry

## Example Usage

```typescript
import { EnvironmentEntryInput } from "@gram/client/models/components";

let value: EnvironmentEntryInput = {
  name: "<value>",
  value: "<value>",
};
```

## Fields

| Field                                 | Type                                  | Required                              | Description                           |
| ------------------------------------- | ------------------------------------- | ------------------------------------- | ------------------------------------- |
| `name`                                | *string*                              | :heavy_check_mark:                    | The name of the environment variable  |
| `value`                               | *string*                              | :heavy_check_mark:                    | The value of the environment variable |