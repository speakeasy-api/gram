# GetProductFeaturesResponseBody

## Example Usage

```typescript
import { GetProductFeaturesResponseBody } from "@gram/client/models/components/getproductfeaturesresponsebody.js";

let value: GetProductFeaturesResponseBody = {
  authzChallengeLoggingEnabled: false,
  logsEnabled: true,
  observabilityModeEnabled: true,
  scimEnabled: true,
  sessionCaptureEnabled: true,
  ssoEnabled: false,
  toolIoLogsEnabled: false,
  webhooks: true,
};
```

## Fields

| Field                                                                                   | Type                                                                                    | Required                                                                                | Description                                                                             |
| --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `authzChallengeLoggingEnabled`                                                          | *boolean*                                                                               | :heavy_check_mark:                                                                      | Whether authz challenge logging to ClickHouse is enabled                                |
| `logsEnabled`                                                                           | *boolean*                                                                               | :heavy_check_mark:                                                                      | Whether logging is enabled                                                              |
| `observabilityModeEnabled`                                                              | *boolean*                                                                               | :heavy_check_mark:                                                                      | Whether observability mode is enabled, making generated hook plugins fully non-blocking |
| `scimEnabled`                                                                           | *boolean*                                                                               | :heavy_check_mark:                                                                      | Whether SCIM/directory sync setup is enabled for the organization                       |
| `sessionCaptureEnabled`                                                                 | *boolean*                                                                               | :heavy_check_mark:                                                                      | Whether Claude Code session capture is enabled                                          |
| `ssoEnabled`                                                                            | *boolean*                                                                               | :heavy_check_mark:                                                                      | Whether SSO setup is enabled for the organization                                       |
| `toolIoLogsEnabled`                                                                     | *boolean*                                                                               | :heavy_check_mark:                                                                      | Whether tool I/O logging is enabled                                                     |
| `webhooks`                                                                              | *boolean*                                                                               | :heavy_check_mark:                                                                      | Whether webhooks are enabled                                                            |