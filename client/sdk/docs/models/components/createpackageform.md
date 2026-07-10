# CreatePackageForm

## Example Usage

```typescript
import { CreatePackageForm } from "@gram/client/models/components/createpackageform.js";

let value: CreatePackageForm = {
  name: "<value>",
  summary: "<value>",
  title: "<value>",
};
```

## Fields

| Field          | Type       | Required           | Description                                                           |
| -------------- | ---------- | ------------------ | --------------------------------------------------------------------- |
| `description`  | _string_   | :heavy_minus_sign: | The description of the package. Limited markdown syntax is supported. |
| `imageAssetId` | _string_   | :heavy_minus_sign: | The asset ID of the image to show for this package                    |
| `keywords`     | _string_[] | :heavy_minus_sign: | The keywords of the package                                           |
| `name`         | _string_   | :heavy_check_mark: | The name of the package                                               |
| `summary`      | _string_   | :heavy_check_mark: | The summary of the package                                            |
| `title`        | _string_   | :heavy_check_mark: | The title of the package                                              |
| `url`          | _string_   | :heavy_minus_sign: | External URL for the package owner                                    |
