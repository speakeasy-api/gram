# CreateSlackAppRequestBody

## Example Usage

```typescript
import { CreateSlackAppRequestBody } from "@gram/client/models/components";

let value: CreateSlackAppRequestBody = {
  name: "<value>",
  toolsetIds: [
    "<value 1>",
    "<value 2>",
  ],
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `iconAssetId`                     | *string*                          | :heavy_minus_sign:                | Asset ID for the app icon         |
| `name`                            | *string*                          | :heavy_check_mark:                | Display name for the Slack app    |
| `systemPrompt`                    | *string*                          | :heavy_minus_sign:                | System prompt for the Slack app   |
| `toolsetIds`                      | *string*[]                        | :heavy_check_mark:                | Toolset IDs to attach to this app |