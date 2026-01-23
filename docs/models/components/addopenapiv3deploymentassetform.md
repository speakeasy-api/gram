# AddOpenAPIv3DeploymentAssetForm

## Example Usage

```typescript
import { AddOpenAPIv3DeploymentAssetForm } from "@gram/client/models/components";

let value: AddOpenAPIv3DeploymentAssetForm = {
  assetId: "<id>",
  name: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `assetId`                                                       | *string*                                                        | :heavy_check_mark:                                              | The ID of the uploaded asset.                                   |
| `name`                                                          | *string*                                                        | :heavy_check_mark:                                              | The name to give the document as it will be displayed in UIs.   |
| `slug`                                                          | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |