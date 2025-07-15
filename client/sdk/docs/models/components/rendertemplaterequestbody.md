# RenderTemplateRequestBody

## Example Usage

```typescript
import { RenderTemplateRequestBody } from "@gram/client/models/components";

let value: RenderTemplateRequestBody = {
  arguments: {
    "key": "<value>",
  },
  engine: "mustache",
  kind: "prompt",
  prompt: "<value>",
};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `arguments`                                                                                              | Record<string, *any*>                                                                                    | :heavy_check_mark:                                                                                       | The input data to render the template with                                                               |
| `engine`                                                                                                 | [components.RenderTemplateRequestBodyEngine](../../models/components/rendertemplaterequestbodyengine.md) | :heavy_check_mark:                                                                                       | The template engine                                                                                      |
| `kind`                                                                                                   | [components.RenderTemplateRequestBodyKind](../../models/components/rendertemplaterequestbodykind.md)     | :heavy_check_mark:                                                                                       | The kind of prompt the template is used for                                                              |
| `prompt`                                                                                                 | *string*                                                                                                 | :heavy_check_mark:                                                                                       | The template content to render                                                                           |