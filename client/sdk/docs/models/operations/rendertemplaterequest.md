# RenderTemplateRequest

## Example Usage

```typescript
import { RenderTemplateRequest } from "@gram/client/models/operations";

let value: RenderTemplateRequest = {
  renderTemplateRequestBody: {
    arguments: {},
    engine: "mustache",
    kind: "prompt",
    prompt: "<value>",
  },
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                    | *string*                                                                                     | :heavy_minus_sign:                                                                           | API Key header                                                                               |
| `gramSession`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | Session header                                                                               |
| `gramProject`                                                                                | *string*                                                                                     | :heavy_minus_sign:                                                                           | project header                                                                               |
| `renderTemplateRequestBody`                                                                  | [components.RenderTemplateRequestBody](../../models/components/rendertemplaterequestbody.md) | :heavy_check_mark:                                                                           | N/A                                                                                          |