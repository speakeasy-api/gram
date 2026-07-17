# UpdateTemplateRequest

## Example Usage

```typescript
import { UpdateTemplateRequest } from "@gram/client/models/operations/updatetemplate.js";

let value: UpdateTemplateRequest = {
  updatePromptTemplateForm: {
    id: "<id>",
  },
};
```

## Fields

| Field                      | Type                                                                                       | Required           | Description    |
| -------------------------- | ------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`                  | _string_                                                                                   | :heavy_minus_sign: | API Key header |
| `gramSession`              | _string_                                                                                   | :heavy_minus_sign: | Session header |
| `gramProject`              | _string_                                                                                   | :heavy_minus_sign: | project header |
| `updatePromptTemplateForm` | [components.UpdatePromptTemplateForm](../../models/components/updateprompttemplateform.md) | :heavy_check_mark: | N/A            |
