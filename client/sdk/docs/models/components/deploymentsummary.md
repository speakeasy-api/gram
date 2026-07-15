# DeploymentSummary

## Example Usage

```typescript
import { DeploymentSummary } from "@gram/client/models/components/deploymentsummary.js";

let value: DeploymentSummary = {
  createdAt: new Date("2026-12-16T20:51:38.929Z"),
  externalMcpAssetCount: 600883,
  externalMcpToolCount: 297572,
  functionsAssetCount: 782256,
  functionsToolCount: 874847,
  id: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
  openapiv3AssetCount: 979532,
  openapiv3ToolCount: 718578,
  status: "<value>",
  userId: "<id>",
};
```

## Fields

| Field                   | Type                                                                                          | Required           | Description                                                                | Example                                  |
| ----------------------- | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------- | ---------------------------------------- |
| `createdAt`             | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the deployment.                                       |                                          |
| `externalMcpAssetCount` | _number_                                                                                      | :heavy_check_mark: | The number of external MCP server assets.                                  |                                          |
| `externalMcpToolCount`  | _number_                                                                                      | :heavy_check_mark: | The number of tools in the deployment generated from external MCP servers. |                                          |
| `functionsAssetCount`   | _number_                                                                                      | :heavy_check_mark: | The number of Functions assets.                                            |                                          |
| `functionsToolCount`    | _number_                                                                                      | :heavy_check_mark: | The number of tools in the deployment generated from Functions.            |                                          |
| `id`                    | _string_                                                                                      | :heavy_check_mark: | The ID to of the deployment.                                               | bc5f4a555e933e6861d12edba4c2d87ef6caf8e6 |
| `openapiv3AssetCount`   | _number_                                                                                      | :heavy_check_mark: | The number of upstream OpenAPI assets.                                     |                                          |
| `openapiv3ToolCount`    | _number_                                                                                      | :heavy_check_mark: | The number of tools in the deployment generated from OpenAPI documents.    |                                          |
| `status`                | _string_                                                                                      | :heavy_check_mark: | The status of the deployment.                                              |                                          |
| `userId`                | _string_                                                                                      | :heavy_check_mark: | The ID of the user that created the deployment.                            |                                          |
