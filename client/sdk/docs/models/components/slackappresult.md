# SlackAppResult

## Example Usage

```typescript
import { SlackAppResult } from "@gram/client/models/components";

let value: SlackAppResult = {
  createdAt: new Date("2024-10-02T00:18:09.002Z"),
  id: "481a3998-2413-43ef-9abd-f8fe063cd455",
  name: "<value>",
  status: "<value>",
  toolsetIds: [
    "<value 1>",
  ],
  updatedAt: new Date("2025-12-11T08:29:04.069Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `iconAssetId`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Asset ID for the app icon                                                                     |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The Slack app ID                                                                              |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Display name of the Slack app                                                                 |
| `redirectUrl`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | OAuth callback URL for this app                                                               |
| `requestUrl`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Event subscription URL for this app                                                           |
| `slackClientId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The Slack app Client ID                                                                       |
| `slackTeamId`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The connected Slack workspace ID                                                              |
| `slackTeamName`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The connected Slack workspace name                                                            |
| `status`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | Current status: unconfigured, active                                                          |
| `systemPrompt`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | System prompt for the Slack app                                                               |
| `toolsetIds`                                                                                  | *string*[]                                                                                    | :heavy_check_mark:                                                                            | Attached toolset IDs                                                                          |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |