# ResourceEntry

## Example Usage

```typescript
import { ResourceEntry } from "@gram/client/models/components/resourceentry.js";

let value: ResourceEntry = {
  id: "<id>",
  name: "<value>",
  resourceUrn: "<value>",
  type: "function",
  uri: "https://sweet-premise.org/",
};
```

## Fields

| Field         | Type                                                                         | Required           | Description              |
| ------------- | ---------------------------------------------------------------------------- | ------------------ | ------------------------ |
| `id`          | _string_                                                                     | :heavy_check_mark: | The ID of the resource   |
| `name`        | _string_                                                                     | :heavy_check_mark: | The name of the resource |
| `resourceUrn` | _string_                                                                     | :heavy_check_mark: | The URN of the resource  |
| `type`        | [components.ResourceEntryType](../../models/components/resourceentrytype.md) | :heavy_check_mark: | N/A                      |
| `uri`         | _string_                                                                     | :heavy_check_mark: | The uri of the resource  |
