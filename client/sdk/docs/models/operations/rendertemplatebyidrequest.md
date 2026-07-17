# RenderTemplateByIDRequest

## Example Usage

```typescript
import { RenderTemplateByIDRequest } from "@gram/client/models/operations/rendertemplatebyid.js";

let value: RenderTemplateByIDRequest = {
  id: "<id>",
  renderTemplateByIDRequestBody: {
    arguments: {},
  },
};
```

## Fields

| Field                           | Type                                                                                                 | Required           | Description                             |
| ------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------- |
| `id`                            | _string_                                                                                             | :heavy_check_mark: | The ID of the prompt template to render |
| `gramKey`                       | _string_                                                                                             | :heavy_minus_sign: | API Key header                          |
| `gramSession`                   | _string_                                                                                             | :heavy_minus_sign: | Session header                          |
| `gramProject`                   | _string_                                                                                             | :heavy_minus_sign: | project header                          |
| `renderTemplateByIDRequestBody` | [components.RenderTemplateByIDRequestBody](../../models/components/rendertemplatebyidrequestbody.md) | :heavy_check_mark: | N/A                                     |
