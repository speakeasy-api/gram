# ToolEntry

## Example Usage

```typescript
import { ToolEntry } from "@gram/client/models/components/toolentry.js";

let value: ToolEntry = {
  id: "<id>",
  name: "<value>",
  toolUrn: "<value>",
  type: "externalmcp",
};
```

## Fields

| Field         | Type                                                                     | Required           | Description                                                |
| ------------- | ------------------------------------------------------------------------ | ------------------ | ---------------------------------------------------------- |
| `annotations` | [components.ToolAnnotations](../../models/components/toolannotations.md) | :heavy_minus_sign: | Tool annotations providing behavioral hints about the tool |
| `httpMethod`  | _string_                                                                 | :heavy_minus_sign: | HTTP method for HTTP tools (GET, POST, PUT, PATCH, DELETE) |
| `id`          | _string_                                                                 | :heavy_check_mark: | The ID of the tool                                         |
| `name`        | _string_                                                                 | :heavy_check_mark: | The name of the tool                                       |
| `toolUrn`     | _string_                                                                 | :heavy_check_mark: | The URN of the tool                                        |
| `type`        | [components.ToolEntryType](../../models/components/toolentrytype.md)     | :heavy_check_mark: | The type of tool                                           |
