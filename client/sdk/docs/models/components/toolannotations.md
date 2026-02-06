# ToolAnnotations

MCP tool annotations providing hints about tool behavior

## Example Usage

```typescript
import { ToolAnnotations } from "@gram/client/models/components";

let value: ToolAnnotations = {};
```

## Fields

| Field                                                                                                                | Type                                                                                                                 | Required                                                                                                             | Description                                                                                                          |
| -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `destructiveHint`                                                                                                    | *boolean*                                                                                                            | :heavy_minus_sign:                                                                                                   | If true, the tool may perform destructive updates (only meaningful when read_only_hint is false)                     |
| `idempotentHint`                                                                                                     | *boolean*                                                                                                            | :heavy_minus_sign:                                                                                                   | If true, repeated calls with same arguments have no additional effect (only meaningful when read_only_hint is false) |
| `openWorldHint`                                                                                                      | *boolean*                                                                                                            | :heavy_minus_sign:                                                                                                   | If true, the tool interacts with external entities beyond its local environment                                      |
| `readOnlyHint`                                                                                                       | *boolean*                                                                                                            | :heavy_minus_sign:                                                                                                   | If true, the tool does not modify its environment                                                                    |
| `title`                                                                                                              | *string*                                                                                                             | :heavy_minus_sign:                                                                                                   | Human-readable display name for the tool                                                                             |