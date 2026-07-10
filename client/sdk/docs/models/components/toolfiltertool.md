# ToolFilterTool

A tool referenced by a tool filter scope, identified by URN and display name.

## Example Usage

```typescript
import { ToolFilterTool } from "@gram/client/models/components/toolfiltertool.js";

let value: ToolFilterTool = {
  name: "<value>",
  toolUrn: "<value>",
};
```

## Fields

| Field     | Type     | Required           | Description                                                                                                         |
| --------- | -------- | ------------------ | ------------------------------------------------------------------------------------------------------------------- |
| `name`    | _string_ | :heavy_check_mark: | The display name of the tool, with any variation rename from the resolved group applied (matching the runtime wire) |
| `toolUrn` | _string_ | :heavy_check_mark: | The URN of the tool                                                                                                 |
