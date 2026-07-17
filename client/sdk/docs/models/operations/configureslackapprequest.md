# ConfigureSlackAppRequest

## Example Usage

```typescript
import { ConfigureSlackAppRequest } from "@gram/client/models/operations";

let value: ConfigureSlackAppRequest = {
  configureSlackAppRequestBody: {
    id: "a7e5c2de-5c40-4b73-8520-8fa5fdd9dc90",
    slackClientId: "<id>",
    slackClientSecret: "<value>",
    slackSigningSecret: "<value>",
  },
};
```

## Fields

| Field                          | Type                                                                                               | Required           | Description    |
| ------------------------------ | -------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`                  | _string_                                                                                           | :heavy_minus_sign: | Session header |
| `gramProject`                  | _string_                                                                                           | :heavy_minus_sign: | project header |
| `configureSlackAppRequestBody` | [components.ConfigureSlackAppRequestBody](../../models/components/configureslackapprequestbody.md) | :heavy_check_mark: | N/A            |
