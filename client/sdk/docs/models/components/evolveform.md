# EvolveForm

## Example Usage

```typescript
import { EvolveForm } from "@gram/client/models/components/evolveform.js";

let value: EvolveForm = {
  nonBlocking: false,
  upsertExternalMcps: [
    {
      name: "My Slack Integration",
      registryServerSpecifier: "slack",
      selectedRemotes: ["https://mcp.example.com/sse"],
      slug: "<value>",
    },
  ],
};
```

## Fields

| Field                    | Type                                                                                                       | Required           | Description                                                                                                                                            | Example |
| ------------------------ | ---------------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ------- |
| `deploymentId`           | _string_                                                                                                   | :heavy_minus_sign: | The ID of the deployment to evolve. If omitted, the latest deployment will be used.                                                                    |         |
| `excludeExternalMcps`    | _string_[]                                                                                                 | :heavy_minus_sign: | The external MCP servers, identified by slug, to exclude from the new deployment when cloning a previous deployment.                                   |         |
| `excludeFunctions`       | _string_[]                                                                                                 | :heavy_minus_sign: | The functions, identified by slug, to exclude from the new deployment when cloning a previous deployment.                                              |         |
| `excludeOpenapiv3Assets` | _string_[]                                                                                                 | :heavy_minus_sign: | The OpenAPI 3.x documents, identified by slug, to exclude from the new deployment when cloning a previous deployment.                                  |         |
| `excludePackages`        | _string_[]                                                                                                 | :heavy_minus_sign: | The packages to exclude from the new deployment when cloning a previous deployment.                                                                    |         |
| `nonBlocking`            | _boolean_                                                                                                  | :heavy_minus_sign: | If true, the deployment will be created in non-blocking mode where the request will return immediately and the deployment will proceed asynchronously. | false   |
| `upsertExternalMcps`     | [components.AddExternalMCPForm](../../models/components/addexternalmcpform.md)[]                           | :heavy_minus_sign: | The external MCP servers to upsert in the new deployment.                                                                                              |         |
| `upsertFunctions`        | [components.AddFunctionsForm](../../models/components/addfunctionsform.md)[]                               | :heavy_minus_sign: | The tool functions to upsert in the new deployment.                                                                                                    |         |
| `upsertOpenapiv3Assets`  | [components.AddOpenAPIv3DeploymentAssetForm](../../models/components/addopenapiv3deploymentassetform.md)[] | :heavy_minus_sign: | The OpenAPI 3.x documents to upsert in the new deployment.                                                                                             |         |
| `upsertPackages`         | [components.AddPackageForm](../../models/components/addpackageform.md)[]                                   | :heavy_minus_sign: | The packages to upsert in the new deployment.                                                                                                          |         |
