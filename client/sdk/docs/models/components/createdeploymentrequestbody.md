# CreateDeploymentRequestBody

## Example Usage

```typescript
import { CreateDeploymentRequestBody } from "@gram/client/models/components";

let value: CreateDeploymentRequestBody = {
  externalId: "bc5f4a555e933e6861d12edba4c2d87ef6caf8e6",
  githubPr: "1234",
  githubRepo: "speakeasyapi/gram",
  githubSha: "f33e693e9e12552043bc0ec5c37f1b8a9e076161",
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            | Example                                                                                                |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `externalId`                                                                                           | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The external ID to refer to the deployment. This can be a git commit hash for example.                 | bc5f4a555e933e6861d12edba4c2d87ef6caf8e6                                                               |
| `externalUrl`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The upstream URL a deployment can refer to. This can be a github url to a commit hash or pull request. |                                                                                                        |
| `githubPr`                                                                                             | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The github pull request that resulted in the deployment.                                               | 1234                                                                                                   |
| `githubRepo`                                                                                           | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The github repository in the form of "owner/repo".                                                     | speakeasyapi/gram                                                                                      |
| `githubSha`                                                                                            | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The commit hash that triggered the deployment.                                                         | f33e693e9e12552043bc0ec5c37f1b8a9e076161                                                               |
| `openapiv3Assets`                                                                                      | [components.OpenAPIv3DeploymentAssetForm](../../models/components/openapiv3deploymentassetform.md)[]   | :heavy_minus_sign:                                                                                     | N/A                                                                                                    |                                                                                                        |