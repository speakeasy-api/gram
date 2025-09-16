# HTTPToolDefinitionEntry

## Example Usage

```typescript
import { HTTPToolDefinitionEntry } from "@gram/client/models/components";

let value: HTTPToolDefinitionEntry = {
  id: "<id>",
  name: "<value>",
  toolType: "http",
};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                     | *string*                                                                                                 | :heavy_check_mark:                                                                                       | The ID of the HTTP tool                                                                                  |
| `name`                                                                                                   | *string*                                                                                                 | :heavy_check_mark:                                                                                       | The name of the tool                                                                                     |
| `toolType`                                                                                               | [components.HTTPToolDefinitionEntryToolType](../../models/components/httptooldefinitionentrytooltype.md) | :heavy_check_mark:                                                                                       | N/A                                                                                                      |