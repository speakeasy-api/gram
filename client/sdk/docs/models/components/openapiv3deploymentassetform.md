# OpenAPIv3DeploymentAssetForm

## Example Usage

```typescript
import { OpenAPIv3DeploymentAssetForm } from "@gram/client/models/components";

let value: OpenAPIv3DeploymentAssetForm = {
  assetId: "<id>",
  name: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `assetId`                                                      | *string*                                                       | :heavy_check_mark:                                             | The ID of the uploaded asset.                                  |
| `name`                                                         | *string*                                                       | :heavy_check_mark:                                             | The name to give the document as it will be displayed in UIs.  |
| `slug`                                                         | *string*                                                       | :heavy_check_mark:                                             | The slug to give the document as it will be displayed in URLs. |