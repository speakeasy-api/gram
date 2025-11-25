# ToolsetEnvironmentLink

A link between a toolset and an environment

## Example Usage

```typescript
import { ToolsetEnvironmentLink } from "@gram/client/models/components";

let value: ToolsetEnvironmentLink = {
  environmentId: "45cdbda5-ffcb-4676-ae4f-91767f82c5ee",
  id: "e6a27113-d1b0-4dc4-9efb-3b190cfe13f3",
  toolsetId: "1db55af2-474b-45d0-a69f-4881008ab261",
};
```

## Fields

| Field                                  | Type                                   | Required                               | Description                            |
| -------------------------------------- | -------------------------------------- | -------------------------------------- | -------------------------------------- |
| `environmentId`                        | *string*                               | :heavy_check_mark:                     | The ID of the environment              |
| `id`                                   | *string*                               | :heavy_check_mark:                     | The ID of the toolset environment link |
| `toolsetId`                            | *string*                               | :heavy_check_mark:                     | The ID of the toolset                  |