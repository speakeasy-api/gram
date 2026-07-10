# CreateSlackAppRequestBody

## Example Usage

```typescript
import { CreateSlackAppRequestBody } from "@gram/client/models/components";

let value: CreateSlackAppRequestBody = {
  name: "<value>",
  toolsetIds: ["<value 1>", "<value 2>"],
};
```

## Fields

| Field          | Type       | Required           | Description                       |
| -------------- | ---------- | ------------------ | --------------------------------- |
| `iconAssetId`  | _string_   | :heavy_minus_sign: | Asset ID for the app icon         |
| `name`         | _string_   | :heavy_check_mark: | Display name for the Slack app    |
| `systemPrompt` | _string_   | :heavy_minus_sign: | System prompt for the Slack app   |
| `toolsetIds`   | _string_[] | :heavy_check_mark: | Toolset IDs to attach to this app |
