# SetToolsetEnvironmentLinkRequestBody

## Example Usage

```typescript
import { SetToolsetEnvironmentLinkRequestBody } from "@gram/client/models/components";

let value: SetToolsetEnvironmentLinkRequestBody = {
  environmentId: "4ea7ea65-99e2-4454-9b9a-1bdef0c250fa",
  toolsetId: "8483e627-b1e9-4553-ae64-20a1d9a794b8",
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `environmentId`                   | *string*                          | :heavy_check_mark:                | The ID of the environment to link |
| `toolsetId`                       | *string*                          | :heavy_check_mark:                | The ID of the toolset             |