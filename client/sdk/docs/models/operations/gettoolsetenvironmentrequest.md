# GetToolsetEnvironmentRequest

## Example Usage

```typescript
import { GetToolsetEnvironmentRequest } from "@gram/client/models/operations";

let value: GetToolsetEnvironmentRequest = {
  toolsetId: "69004b23-a837-4dd5-8360-195155787cae",
};
```

## Fields

| Field                 | Type                  | Required              | Description           |
| --------------------- | --------------------- | --------------------- | --------------------- |
| `toolsetId`           | *string*              | :heavy_check_mark:    | The ID of the toolset |
| `gramSession`         | *string*              | :heavy_minus_sign:    | Session header        |
| `gramProject`         | *string*              | :heavy_minus_sign:    | project header        |