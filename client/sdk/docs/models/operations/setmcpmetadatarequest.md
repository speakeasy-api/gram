# SetMcpMetadataRequest

## Example Usage

```typescript
import { SetMcpMetadataRequest } from "@gram/client/models/operations";

let value: SetMcpMetadataRequest = {
  setMcpMetadataRequestBody: {
    toolsetSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                    | *string*                                                                                     | :heavy_minus_sign:                                                                           | API Key header                                                                               |
| `gramSession`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | Session header                                                                               |
| `gramProject`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | project header                                                                               |
| `setMcpMetadataRequestBody`                                                                  | [components.SetMcpMetadataRequestBody](../../models/components/setmcpmetadatarequestbody.md) | :heavy_check_mark:                                                                           | N/A                                                                                          |