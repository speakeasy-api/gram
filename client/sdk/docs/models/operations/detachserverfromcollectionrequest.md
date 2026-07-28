# DetachServerFromCollectionRequest

## Example Usage

```typescript
import { DetachServerFromCollectionRequest } from "@gram/client/models/operations/detachserverfromcollection.js";

let value: DetachServerFromCollectionRequest = {
  attachServerRequestBody: {
    collectionId: "e68e28b9-772d-4f7a-ac89-7045f2a02ee4",
  },
};
```

## Fields

| Field                     | Type                                                                                     | Required           | Description    |
| ------------------------- | ---------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`             | _string_                                                                                 | :heavy_minus_sign: | Session header |
| `gramKey`                 | _string_                                                                                 | :heavy_minus_sign: | API Key header |
| `attachServerRequestBody` | [components.AttachServerRequestBody](../../models/components/attachserverrequestbody.md) | :heavy_check_mark: | N/A            |
