# AttachServerToCollectionRequest

## Example Usage

```typescript
import { AttachServerToCollectionRequest } from "@gram/client/models/operations/attachservertocollection.js";

let value: AttachServerToCollectionRequest = {
  attachServerRequestBody: {
    collectionId: "e68e28b9-772d-4f7a-ac89-7045f2a02ee4",
  },
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `gramSession`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | Session header                                                                           |
| `gramKey`                                                                                | *string*                                                                                 | :heavy_minus_sign:                                                                       | API Key header                                                                           |
| `attachServerRequestBody`                                                                | [components.AttachServerRequestBody](../../models/components/attachserverrequestbody.md) | :heavy_check_mark:                                                                       | N/A                                                                                      |