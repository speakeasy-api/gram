# GramProductFeatures

Current state of product feature flags

## Example Usage

```typescript
import { GramProductFeatures } from "@gram/client/models/components";

let value: GramProductFeatures = {
  authzChallengeLoggingEnabled: true,
  logsEnabled: false,
  sessionCaptureEnabled: false,
  toolIoLogsEnabled: true,
};
```

## Fields

| Field                          | Type      | Required           | Description                                              |
| ------------------------------ | --------- | ------------------ | -------------------------------------------------------- |
| `authzChallengeLoggingEnabled` | _boolean_ | :heavy_check_mark: | Whether authz challenge logging to ClickHouse is enabled |
| `logsEnabled`                  | _boolean_ | :heavy_check_mark: | Whether logging is enabled                               |
| `sessionCaptureEnabled`        | _boolean_ | :heavy_check_mark: | Whether Claude Code session capture is enabled           |
| `toolIoLogsEnabled`            | _boolean_ | :heavy_check_mark: | Whether tool I/O logging is enabled                      |
