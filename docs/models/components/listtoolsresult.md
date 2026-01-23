# ListToolsResult

## Example Usage

```typescript
import { ListToolsResult } from "@gram/client/models/components";

let value: ListToolsResult = {
  tools: [
    {},
  ],
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `nextCursor`                                                             | *string*                                                                 | :heavy_minus_sign:                                                       | The cursor to fetch results from                                         |
| `tools`                                                                  | [components.Tool](../../models/components/tool.md)[]                     | :heavy_check_mark:                                                       | The list of tools (polymorphic union of HTTP tools and prompt templates) |