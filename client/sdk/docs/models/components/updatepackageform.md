# UpdatePackageForm

## Example Usage

```typescript
import { UpdatePackageForm } from "@gram/client/models/components";

let value: UpdatePackageForm = {
  id: "<id>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `description`                                                         | *string*                                                              | :heavy_minus_sign:                                                    | The description of the package. Limited markdown syntax is supported. |
| `id`                                                                  | *string*                                                              | :heavy_check_mark:                                                    | The id of the package to update                                       |
| `imageAssetId`                                                        | *string*                                                              | :heavy_minus_sign:                                                    | The asset ID of the image to show for this package                    |
| `keywords`                                                            | *string*[]                                                            | :heavy_minus_sign:                                                    | The keywords of the package                                           |
| `summary`                                                             | *string*                                                              | :heavy_minus_sign:                                                    | The summary of the package                                            |
| `title`                                                               | *string*                                                              | :heavy_minus_sign:                                                    | The title of the package                                              |
| `url`                                                                 | *string*                                                              | :heavy_minus_sign:                                                    | External URL for the package owner                                    |