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

| Field                          | Type      | Required           | Description                                                                             |
| ------------------------------ | --------- | ------------------ | --------------------------------------------------------------------------------------- |
| `authzChallengeLoggingEnabled` | _boolean_ | :heavy_check_mark: | Whether authz challenge logging to ClickHouse is enabled                                |
| `logsEnabled`                  | _boolean_ | :heavy_check_mark: | Whether logging is enabled                                                              |
| `observabilityModeEnabled`     | _boolean_ | :heavy_check_mark: | Whether observability mode is enabled, making generated hook plugins fully non-blocking |
| `scimEnabled`                  | _boolean_ | :heavy_check_mark: | Whether SCIM/directory sync setup is enabled for the organization                       |
| `sessionCaptureEnabled`        | _boolean_ | :heavy_check_mark: | Whether Claude Code session capture is enabled                                          |
| `ssoEnabled`                   | _boolean_ | :heavy_check_mark: | Whether SSO setup is enabled for the organization                                       |
| `toolIoLogsEnabled`            | _boolean_ | :heavy_check_mark: | Whether tool I/O logging is enabled                                                     |
| `webhooks`                     | _boolean_ | :heavy_check_mark: | Whether webhooks are enabled                                                            |
