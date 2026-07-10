# ConfigureSlackAppRequestBody

## Example Usage

```typescript
import { ConfigureSlackAppRequestBody } from "@gram/client/models/components";

let value: ConfigureSlackAppRequestBody = {
  id: "f084bddf-6333-4b2f-82de-99db155e585e",
  slackClientId: "<id>",
  slackClientSecret: "<value>",
  slackSigningSecret: "<value>",
};
```

## Fields

| Field                    | Type                     | Required                 | Description              |
| ------------------------ | ------------------------ | ------------------------ | ------------------------ |
| `id`                     | *string*                 | :heavy_check_mark:       | The Slack app ID         |
| `slackClientId`          | *string*                 | :heavy_check_mark:       | Slack app Client ID      |
| `slackClientSecret`      | *string*                 | :heavy_check_mark:       | Slack app Client Secret  |
| `slackSigningSecret`     | *string*                 | :heavy_check_mark:       | Slack app Signing Secret |