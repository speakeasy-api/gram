# RenderTemplateByIDRequest

## Example Usage

```typescript
import { RenderTemplateByIDRequest } from "@gram/client/models/operations";

let value: RenderTemplateByIDRequest = {
  id: "<id>",
  renderTemplateByIDRequestBody: {
    arguments: {},
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `id`                                                                                                 | *string*                                                                                             | :heavy_check_mark:                                                                                   | The ID of the prompt template to render                                                              |
| `gramKey`                                                                                            | *string*                                                                                             | :heavy_minus_sign:                                                                                   | API Key header                                                                                       |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `gramProject`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | project header                                                                                       |
| `renderTemplateByIDRequestBody`                                                                      | [components.RenderTemplateByIDRequestBody](../../models/components/rendertemplatebyidrequestbody.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |