# GetSlackConnectionResult

## Example Usage

```typescript
import { GetSlackConnectionResult } from "@gram/client/models/components";

let value: GetSlackConnectionResult = {
  createdAt: new Date("2024-12-04T11:14:15.948Z"),
  defaultToolsetSlug: "<value>",
  slackTeamId: "<id>",
  slackTeamName: "<value>",
  updatedAt: new Date("2026-05-05T16:38:36.066Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was created.                                                                 |
| `defaultToolsetSlug`                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The default toolset slug for this Slack connection                                            |
| `slackTeamId`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the connected Slack team                                                            |
| `slackTeamName`                                                                               | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the connected Slack team                                                          |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was last updated.                                                            |