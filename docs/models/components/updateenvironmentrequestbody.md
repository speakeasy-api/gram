# UpdateEnvironmentRequestBody

## Example Usage

```typescript
import { UpdateEnvironmentRequestBody } from "@gram/client/models/components";

let value: UpdateEnvironmentRequestBody = {
  entriesToRemove: [
    "<value 1>",
    "<value 2>",
  ],
  entriesToUpdate: [],
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `description`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | The description of the environment                                                     |
| `entriesToRemove`                                                                      | *string*[]                                                                             | :heavy_check_mark:                                                                     | List of environment entry names to remove                                              |
| `entriesToUpdate`                                                                      | [components.EnvironmentEntryInput](../../models/components/environmententryinput.md)[] | :heavy_check_mark:                                                                     | List of environment entries to update or create                                        |
| `entryDisplayNamesToRemove`                                                            | *string*[]                                                                             | :heavy_minus_sign:                                                                     | Entry names to remove display names from                                               |
| `entryDisplayNamesToUpdate`                                                            | Record<string, *string*>                                                               | :heavy_minus_sign:                                                                     | Map of entry names to display names to set or update                                   |
| `name`                                                                                 | *string*                                                                               | :heavy_minus_sign:                                                                     | The name of the environment                                                            |