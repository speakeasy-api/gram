# DownloadPluginPackageRequest

## Example Usage

```typescript
import { DownloadPluginPackageRequest } from "@gram/client/models/operations/downloadpluginpackage.js";

let value: DownloadPluginPackageRequest = {
  pluginId: "72af88d1-7699-4437-b83a-26ba9d74458a",
  platform: "codex",
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `pluginId`                                                                     | *string*                                                                       | :heavy_check_mark:                                                             | The plugin to download.                                                        |
| `platform`                                                                     | [operations.QueryParamPlatform](../../models/operations/queryparamplatform.md) | :heavy_check_mark:                                                             | Target platform to download plugins for.                                       |
| `gramSession`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | Session header                                                                 |
| `gramProject`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | project header                                                                 |