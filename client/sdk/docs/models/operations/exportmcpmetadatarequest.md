# ExportMcpMetadataRequest

## Example Usage

```typescript
import { ExportMcpMetadataRequest } from "@gram/client/models/operations/exportmcpmetadata.js";

let value: ExportMcpMetadataRequest = {
  exportMcpMetadataRequestBody: {
    mcpSlug: "<value>",
  },
};
```

## Fields

| Field                          | Type                                                                                               | Required           | Description    |
| ------------------------------ | -------------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                      | _string_                                                                                           | :heavy_minus_sign: | API Key header |
| `gramSession`                  | _string_                                                                                           | :heavy_minus_sign: | Session header |
| `gramProject`                  | _string_                                                                                           | :heavy_minus_sign: | project header |
| `exportMcpMetadataRequestBody` | [components.ExportMcpMetadataRequestBody](../../models/components/exportmcpmetadatarequestbody.md) | :heavy_check_mark: | N/A            |
