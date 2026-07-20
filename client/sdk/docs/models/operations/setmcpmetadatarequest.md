# SetMcpMetadataRequest

## Example Usage

```typescript
import { SetMcpMetadataRequest } from "@gram/client/models/operations/setmcpmetadata.js";

let value: SetMcpMetadataRequest = {
  setMcpMetadataRequestBody: {},
};
```

## Fields

| Field                       | Type                                                                                         | Required           | Description    |
| --------------------------- | -------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`                   | _string_                                                                                     | :heavy_minus_sign: | API Key header |
| `gramSession`               | _string_                                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`               | _string_                                                                                     | :heavy_minus_sign: | project header |
| `setMcpMetadataRequestBody` | [components.SetMcpMetadataRequestBody](../../models/components/setmcpmetadatarequestbody.md) | :heavy_check_mark: | N/A            |
