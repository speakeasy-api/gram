# UpdateSlackAppRequestBody

## Example Usage

```typescript
import { UpdateSlackAppRequestBody } from "@gram/client/models/components";

let value: UpdateSlackAppRequestBody = {
  id: "596976a2-d152-427e-a729-f7f5cdf5c2f3",
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `iconAssetId`                      | *string*                           | :heavy_minus_sign:                 | Asset ID for the app icon          |
| `id`                               | *string*                           | :heavy_check_mark:                 | The Slack app ID                   |
| `name`                             | *string*                           | :heavy_minus_sign:                 | New display name for the Slack app |
| `systemPrompt`                     | *string*                           | :heavy_minus_sign:                 | System prompt for the Slack app    |