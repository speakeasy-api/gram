# UpdatePackageForm

## Example Usage

```typescript
import { UpdatePackageForm } from "@gram/client/models/components/updatepackageform.js";

let value: UpdatePackageForm = {
  id: "<id>",
};
```

## Fields

| Field          | Type       | Required           | Description                                                           |
| -------------- | ---------- | ------------------ | --------------------------------------------------------------------- |
| `description`  | _string_   | :heavy_minus_sign: | The description of the package. Limited markdown syntax is supported. |
| `id`           | _string_   | :heavy_check_mark: | The id of the package to update                                       |
| `imageAssetId` | _string_   | :heavy_minus_sign: | The asset ID of the image to show for this package                    |
| `keywords`     | _string_[] | :heavy_minus_sign: | The keywords of the package                                           |
| `summary`      | _string_   | :heavy_minus_sign: | The summary of the package                                            |
| `title`        | _string_   | :heavy_minus_sign: | The title of the package                                              |
| `url`          | _string_   | :heavy_minus_sign: | External URL for the package owner                                    |
