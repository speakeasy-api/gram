# DeleteCollectionRequest

## Example Usage

```typescript
import { DeleteCollectionRequest } from "@gram/client/models/operations/deletecollection.js";

let value: DeleteCollectionRequest = {
  collectionId: "81e7ed52-400a-4e31-a0f2-ad993928f371",
};
```

## Fields

| Field                          | Type                           | Required                       | Description                    |
| ------------------------------ | ------------------------------ | ------------------------------ | ------------------------------ |
| `collectionId`                 | *string*                       | :heavy_check_mark:             | ID of the collection to delete |
| `gramSession`                  | *string*                       | :heavy_minus_sign:             | Session header                 |
| `gramKey`                      | *string*                       | :heavy_minus_sign:             | API Key header                 |