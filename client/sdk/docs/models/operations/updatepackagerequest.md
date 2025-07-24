# UpdatePackageRequest

## Example Usage

```typescript
import { UpdatePackageRequest } from "@gram/client/models/operations";

let value: UpdatePackageRequest = {
  updatePackageForm: {
    id: "<id>",
  },
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `gramKey`                                                                    | *string*                                                                     | :heavy_minus_sign:                                                           | API Key header                                                               |
| `gramSession`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | Session header                                                               |
| `gramProject`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | project header                                                               |
| `updatePackageForm`                                                          | [components.UpdatePackageForm](../../models/components/updatepackageform.md) | :heavy_check_mark:                                                           | N/A                                                                          |