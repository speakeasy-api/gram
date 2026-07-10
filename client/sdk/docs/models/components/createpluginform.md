# CreatePluginForm

## Example Usage

```typescript
import { CreatePluginForm } from "@gram/client/models/components/createpluginform.js";

let value: CreatePluginForm = {
  name: "<value>",
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `description`                                                      | *string*                                                           | :heavy_minus_sign:                                                 | Optional description.                                              |
| `name`                                                             | *string*                                                           | :heavy_check_mark:                                                 | Display name for the plugin.                                       |
| `slug`                                                             | *string*                                                           | :heavy_minus_sign:                                                 | Optional URL-safe identifier. Auto-generated from name if omitted. |