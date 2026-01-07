# CreatePackageForm

## Example Usage

```typescript
import { CreatePackageForm } from "@gram/client/models/components";

let value: CreatePackageForm = {
  name: "<value>",
  summary: "<value>",
  title: "<value>",
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `description`                                                         | *string*                                                              | :heavy_minus_sign:                                                    | The description of the package. Limited markdown syntax is supported. |
| `imageAssetId`                                                        | *string*                                                              | :heavy_minus_sign:                                                    | The asset ID of the image to show for this package                    |
| `keywords`                                                            | *string*[]                                                            | :heavy_minus_sign:                                                    | The keywords of the package                                           |
| `name`                                                                | *string*                                                              | :heavy_check_mark:                                                    | The name of the package                                               |
| `summary`                                                             | *string*                                                              | :heavy_check_mark:                                                    | The summary of the package                                            |
| `title`                                                               | *string*                                                              | :heavy_check_mark:                                                    | The title of the package                                              |
| `url`                                                                 | *string*                                                              | :heavy_minus_sign:                                                    | External URL for the package owner                                    |