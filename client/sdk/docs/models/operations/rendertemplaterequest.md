# RenderTemplateRequest

## Example Usage

```typescript
import { RenderTemplateRequest } from "@gram/client/models/operations";

let value: RenderTemplateRequest = {
  id: "<id>",
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `id`                                                                                         | *string*                                                                                     | :heavy_check_mark:                                                                           | The ID of the prompt template to render                                                      |
| `gramKey`                                                                                    | *string*                                                                                     | :heavy_minus_sign:                                                                           | API Key header                                                                               |
| `gramSession`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | Session header                                                                               |
| `gramProject`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | project header                                                                               |
| `renderTemplateRequestBody`                                                                  | [components.RenderTemplateRequestBody](../../models/components/rendertemplaterequestbody.md) | :heavy_check_mark:                                                                           | N/A                                                                                          |