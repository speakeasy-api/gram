# PromptTemplateEntry

## Example Usage

```typescript
import { PromptTemplateEntry } from "@gram/client/models/components/prompttemplateentry.js";

let value: PromptTemplateEntry = {
  id: "<id>",
  name: "<value>",
};
```

## Fields

| Field  | Type     | Required           | Description                                                     |
| ------ | -------- | ------------------ | --------------------------------------------------------------- |
| `id`   | _string_ | :heavy_check_mark: | The ID of the prompt template                                   |
| `kind` | _string_ | :heavy_minus_sign: | The kind of the prompt template                                 |
| `name` | _string_ | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource. |
