# PromptTemplateEntry

## Example Usage

```typescript
import { PromptTemplateEntry } from "@gram/client/models/components";

let value: PromptTemplateEntry = {
  id: "<id>",
  name: "<value>",
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `id`                                                            | *string*                                                        | :heavy_check_mark:                                              | The ID of the prompt template                                   |
| `kind`                                                          | *string*                                                        | :heavy_minus_sign:                                              | The kind of the prompt template                                 |
| `name`                                                          | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |