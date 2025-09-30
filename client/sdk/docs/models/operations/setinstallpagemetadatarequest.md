# SetInstallPageMetadataRequest

## Example Usage

```typescript
import { SetInstallPageMetadataRequest } from "@gram/client/models/operations";

let value: SetInstallPageMetadataRequest = {
  setInstallPageMetadataRequestBody: {
    toolsetId: "211e5a87-8b2e-4eae-9df2-59df02ef9a61",
  },
};
```

## Fields

| Field                                                                                                        | Type                                                                                                         | Required                                                                                                     | Description                                                                                                  |
| ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | Session header                                                                                               |
| `gramProject`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | project header                                                                                               |
| `setInstallPageMetadataRequestBody`                                                                          | [components.SetInstallPageMetadataRequestBody](../../models/components/setinstallpagemetadatarequestbody.md) | :heavy_check_mark:                                                                                           | N/A                                                                                                          |