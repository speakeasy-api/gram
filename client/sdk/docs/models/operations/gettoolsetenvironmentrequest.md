# GetToolsetEnvironmentRequest

## Example Usage

```typescript
import { GetToolsetEnvironmentRequest } from "@gram/client/models/operations/gettoolsetenvironment.js";

let value: GetToolsetEnvironmentRequest = {
  toolsetId: "69004b23-a837-4dd5-8360-195155787cae",
};
```

## Fields

| Field         | Type     | Required           | Description           |
| ------------- | -------- | ------------------ | --------------------- |
| `toolsetId`   | _string_ | :heavy_check_mark: | The ID of the toolset |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header        |
| `gramProject` | _string_ | :heavy_minus_sign: | project header        |
