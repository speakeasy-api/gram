# CreatePluginRequest

## Example Usage

```typescript
import { CreatePluginRequest } from "@gram/client/models/operations/createplugin.js";

let value: CreatePluginRequest = {
  createPluginForm: {
    name: "<value>",
  },
};
```

## Fields

| Field              | Type                                                                       | Required           | Description    |
| ------------------ | -------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`      | _string_                                                                   | :heavy_minus_sign: | Session header |
| `gramProject`      | _string_                                                                   | :heavy_minus_sign: | project header |
| `createPluginForm` | [components.CreatePluginForm](../../models/components/createpluginform.md) | :heavy_check_mark: | N/A            |
