# UpdatePackageRequest

## Example Usage

```typescript
import { UpdatePackageRequest } from "@gram/client/models/operations/updatepackage.js";

let value: UpdatePackageRequest = {
  updatePackageForm: {
    id: "<id>",
  },
};
```

## Fields

| Field               | Type                                                                         | Required           | Description    |
| ------------------- | ---------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`           | _string_                                                                     | :heavy_minus_sign: | API Key header |
| `gramSession`       | _string_                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`       | _string_                                                                     | :heavy_minus_sign: | project header |
| `updatePackageForm` | [components.UpdatePackageForm](../../models/components/updatepackageform.md) | :heavy_check_mark: | N/A            |
