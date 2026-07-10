# UpdateRequestBody

## Example Usage

```typescript
import { UpdateRequestBody } from "@gram/client/models/components/updaterequestbody.js";

let value: UpdateRequestBody = {
  collectionId: "356aaae6-935a-4e89-9727-ed91b869d5c4",
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `collectionId`                                                                                   | *string*                                                                                         | :heavy_check_mark:                                                                               | ID of the collection to update                                                                   |
| `description`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Description of the collection                                                                    |
| `name`                                                                                           | *string*                                                                                         | :heavy_minus_sign:                                                                               | Display name for the collection                                                                  |
| `visibility`                                                                                     | [components.UpdateRequestBodyVisibility](../../models/components/updaterequestbodyvisibility.md) | :heavy_minus_sign:                                                                               | Visibility of the collection                                                                     |