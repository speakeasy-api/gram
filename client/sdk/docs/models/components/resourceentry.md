# ResourceEntry

## Example Usage

```typescript
import { ResourceEntry } from "@gram/client/models/components";

let value: ResourceEntry = {
  id: "<id>",
  name: "<value>",
  resourceUrn: "<value>",
  type: "function",
  uri: "https://sweet-premise.org/",
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `id`                                                                         | *string*                                                                     | :heavy_check_mark:                                                           | The ID of the resource                                                       |
| `name`                                                                       | *string*                                                                     | :heavy_check_mark:                                                           | The name of the resource                                                     |
| `resourceUrn`                                                                | *string*                                                                     | :heavy_check_mark:                                                           | The URN of the resource                                                      |
| `type`                                                                       | [components.ResourceEntryType](../../models/components/resourceentrytype.md) | :heavy_check_mark:                                                           | N/A                                                                          |
| `uri`                                                                        | *string*                                                                     | :heavy_check_mark:                                                           | The uri of the resource                                                      |