# UpsertGlobalToolVariationForm

## Example Usage

```typescript
import { UpsertGlobalToolVariationForm } from "@gram/client/models/components/upsertglobaltoolvariationform.js";

let value: UpsertGlobalToolVariationForm = {
  srcToolName: "<value>",
  srcToolUrn: "<value>",
};
```

## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `confirm`                                                    | [components.Confirm](../../models/components/confirm.md)     | :heavy_minus_sign:                                           | The confirmation mode for the tool variation                 |
| `confirmPrompt`                                              | *string*                                                     | :heavy_minus_sign:                                           | The confirmation prompt for the tool variation               |
| `description`                                                | *string*                                                     | :heavy_minus_sign:                                           | The description of the tool variation                        |
| `destructiveHint`                                            | *boolean*                                                    | :heavy_minus_sign:                                           | Override: if true, the tool may perform destructive updates  |
| `idempotentHint`                                             | *boolean*                                                    | :heavy_minus_sign:                                           | Override: if true, repeated calls have no additional effect  |
| `name`                                                       | *string*                                                     | :heavy_minus_sign:                                           | The name of the tool variation                               |
| `openWorldHint`                                              | *boolean*                                                    | :heavy_minus_sign:                                           | Override: if true, the tool interacts with external entities |
| `readOnlyHint`                                               | *boolean*                                                    | :heavy_minus_sign:                                           | Override: if true, the tool does not modify its environment  |
| `srcToolName`                                                | *string*                                                     | :heavy_check_mark:                                           | The name of the source tool                                  |
| `srcToolUrn`                                                 | *string*                                                     | :heavy_check_mark:                                           | The URN of the source tool                                   |
| `summarizer`                                                 | *string*                                                     | :heavy_minus_sign:                                           | The summarizer of the tool variation                         |
| `summary`                                                    | *string*                                                     | :heavy_minus_sign:                                           | The summary of the tool variation                            |
| `tags`                                                       | *string*[]                                                   | :heavy_minus_sign:                                           | The tags of the tool variation                               |
| `title`                                                      | *string*                                                     | :heavy_minus_sign:                                           | Display name override for the tool                           |