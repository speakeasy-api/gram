# SetInstallPageMetadataRequest

## Example Usage

```typescript
import { SetInstallPageMetadataRequest } from "@gram/client/models/operations";

let value: SetInstallPageMetadataRequest = {
  setInstallPageMetadataRequestBody: {
    toolsetSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                                        | Type                                                                                                         | Required                                                                                                     | Description                                                                                                  |
| ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | Session header                                                                                               |
| `gramProject`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | project header                                                                                               |
| `setInstallPageMetadataRequestBody`                                                                          | [components.SetInstallPageMetadataRequestBody](../../models/components/setinstallpagemetadatarequestbody.md) | :heavy_check_mark:                                                                                           | N/A                                                                                                          |