# ExportMcpMetadataRequest

## Example Usage

```typescript
import { ExportMcpMetadataRequest } from "@gram/client/models/operations";

let value: ExportMcpMetadataRequest = {
  exportMcpMetadataRequestBody: {
    toolsetSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `gramProject`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | project header                                                                                     |
| `exportMcpMetadataRequestBody`                                                                     | [components.ExportMcpMetadataRequestBody](../../models/components/exportmcpmetadatarequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |