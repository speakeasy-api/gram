# SwitchEditingModeRequestBody

## Example Usage

```typescript
import { SwitchEditingModeRequestBody } from "@gram/client/models/components";

let value: SwitchEditingModeRequestBody = {
  mode: "staging",
  slug: "<value>",
};
```

## Fields

| Field                                              | Type                                               | Required                                           | Description                                        |
| -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- | -------------------------------------------------- |
| `mode`                                             | [components.Mode](../../models/components/mode.md) | :heavy_check_mark:                                 | The editing mode: 'iteration' or 'staging'         |
| `slug`                                             | *string*                                           | :heavy_check_mark:                                 | The slug of the toolset                            |