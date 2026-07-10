# DownloadObservabilityPluginRequest

## Example Usage

```typescript
import { DownloadObservabilityPluginRequest } from "@gram/client/models/operations/downloadobservabilityplugin.js";

let value: DownloadObservabilityPluginRequest = {
  platform: "codex",
};
```

## Fields

| Field                                                      | Type                                                       | Required                                                   | Description                                                |
| ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------- |
| `platform`                                                 | [operations.Platform](../../models/operations/platform.md) | :heavy_check_mark:                                         | Target platform.                                           |
| `gramSession`                                              | *string*                                                   | :heavy_minus_sign:                                         | Session header                                             |
| `gramProject`                                              | *string*                                                   | :heavy_minus_sign:                                         | project header                                             |