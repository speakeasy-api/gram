# ToolFilterScope

A filter tag ("scope") and the tools reachable when filtering by it via the runtime ?tags= parameter.

## Example Usage

```typescript
import { ToolFilterScope } from "@gram/client/models/components/toolfilterscope.js";

let value: ToolFilterScope = {
  tag: "<value>",
  toolCount: 741112,
  tools: [],
};
```

## Fields

| Field       | Type                                                                     | Required           | Description                          |
| ----------- | ------------------------------------------------------------------------ | ------------------ | ------------------------------------ |
| `tag`       | _string_                                                                 | :heavy_check_mark: | The filter tag                       |
| `toolCount` | _number_                                                                 | :heavy_check_mark: | The number of tools under this scope |
| `tools`     | [components.ToolFilterTool](../../models/components/toolfiltertool.md)[] | :heavy_check_mark: | The tools under this scope           |
