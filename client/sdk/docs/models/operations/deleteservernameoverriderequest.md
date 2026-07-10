# DeleteServerNameOverrideRequest

## Example Usage

```typescript
import { DeleteServerNameOverrideRequest } from "@gram/client/models/operations/deleteservernameoverride.js";

let value: DeleteServerNameOverrideRequest = {
  deleteRequestBody: {
    overrideId: "<id>",
  },
};
```

## Fields

| Field               | Type                                                                         | Required           | Description    |
| ------------------- | ---------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`           | _string_                                                                     | :heavy_minus_sign: | API Key header |
| `gramSession`       | _string_                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`       | _string_                                                                     | :heavy_minus_sign: | project header |
| `deleteRequestBody` | [components.DeleteRequestBody](../../models/components/deleterequestbody.md) | :heavy_check_mark: | N/A            |
