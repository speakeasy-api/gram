# UpdateCollectionRequest

## Example Usage

```typescript
import { UpdateCollectionRequest } from "@gram/client/models/operations/updatecollection.js";

let value: UpdateCollectionRequest = {
  updateRequestBody: {
    collectionId: "d6dab698-8247-4f44-8b35-06e0969973e2",
  },
};
```

## Fields

| Field               | Type                                                                         | Required           | Description    |
| ------------------- | ---------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`       | _string_                                                                     | :heavy_minus_sign: | Session header |
| `gramKey`           | _string_                                                                     | :heavy_minus_sign: | API Key header |
| `updateRequestBody` | [components.UpdateRequestBody](../../models/components/updaterequestbody.md) | :heavy_check_mark: | N/A            |
