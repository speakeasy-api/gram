# CreatePluginForm

## Example Usage

```typescript
import { CreatePluginForm } from "@gram/client/models/components/createpluginform.js";

let value: CreatePluginForm = {
  name: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description                                                        |
| ------------- | -------- | ------------------ | ------------------------------------------------------------------ |
| `description` | _string_ | :heavy_minus_sign: | Optional description.                                              |
| `name`        | _string_ | :heavy_check_mark: | Display name for the plugin.                                       |
| `slug`        | _string_ | :heavy_minus_sign: | Optional URL-safe identifier. Auto-generated from name if omitted. |
