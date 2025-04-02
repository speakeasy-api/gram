# CreateDeploymentForm

## Example Usage

```typescript
import { CreateDeploymentForm } from "@gram/sdk/models/components";

let value: CreateDeploymentForm = {
  externalId: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
  externalUrl: "Hic dolorem necessitatibus rerum sit.",
  githubRepo: "speakeasyapi/gram",
  githubSha: "f33e693e9e12552043bc0ec5c37f1b8a9e076161",
  idempotencyKey: "01jqq0ajmb4qh9eppz48dejr2m",
  openapiv3AssetIds: [
    "Amet qui natus porro iure sint.",
    "Soluta esse ipsam eligendi.",
    "Laborum fuga sequi est magni.",
    "Dicta ut in non.",
  ],
};
```

## Fields

| Field                                                                                                                                 | Type                                                                                                                                  | Required                                                                                                                              | Description                                                                                                                           | Example                                                                                                                               |
| ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `externalId`                                                                                                                          | *string*                                                                                                                              | :heavy_minus_sign:                                                                                                                    | The external ID to refer to the deployment. This can be a git commit hash for example.                                                | bc5f4a555e933e6861d12edba4c2d87ef6caf8e6                                                                                              |
| `externalUrl`                                                                                                                         | *string*                                                                                                                              | :heavy_minus_sign:                                                                                                                    | The upstream URL a deployment can refer to. This can be a github url to a commit hash or pull request.                                | Ut doloribus perferendis unde provident sed.                                                                                          |
| `githubRepo`                                                                                                                          | *string*                                                                                                                              | :heavy_minus_sign:                                                                                                                    | The github repository in the form of "owner/repo".                                                                                    | speakeasyapi/gram                                                                                                                     |
| `githubSha`                                                                                                                           | *string*                                                                                                                              | :heavy_minus_sign:                                                                                                                    | The commit hash that triggered the deployment.                                                                                        | f33e693e9e12552043bc0ec5c37f1b8a9e076161                                                                                              |
| `idempotencyKey`                                                                                                                      | *string*                                                                                                                              | :heavy_check_mark:                                                                                                                    | A unique identifier that will mitigate against duplicate deployments.                                                                 | 01jqq0ajmb4qh9eppz48dejr2m                                                                                                            |
| `openapiv3AssetIds`                                                                                                                   | *string*[]                                                                                                                            | :heavy_minus_sign:                                                                                                                    | The IDs, as returned from the assets upload service, to uploaded OpenAPI 3.x documents whose operations will become tool definitions. | [<br/>"Est quo modi rerum.",<br/>"Et dolores ut sit non praesentium culpa.",<br/>"Cum rem aut itaque ullam quo."<br/>]                |