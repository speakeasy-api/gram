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

| Field             | Type                                                     | Required           | Description                                                  |
| ----------------- | -------------------------------------------------------- | ------------------ | ------------------------------------------------------------ |
| `confirm`         | [components.Confirm](../../models/components/confirm.md) | :heavy_minus_sign: | The confirmation mode for the tool variation                 |
| `confirmPrompt`   | _string_                                                 | :heavy_minus_sign: | The confirmation prompt for the tool variation               |
| `description`     | _string_                                                 | :heavy_minus_sign: | The description of the tool variation                        |
| `destructiveHint` | _boolean_                                                | :heavy_minus_sign: | Override: if true, the tool may perform destructive updates  |
| `idempotentHint`  | _boolean_                                                | :heavy_minus_sign: | Override: if true, repeated calls have no additional effect  |
| `name`            | _string_                                                 | :heavy_minus_sign: | The name of the tool variation                               |
| `openWorldHint`   | _boolean_                                                | :heavy_minus_sign: | Override: if true, the tool interacts with external entities |
| `readOnlyHint`    | _boolean_                                                | :heavy_minus_sign: | Override: if true, the tool does not modify its environment  |
| `srcToolName`     | _string_                                                 | :heavy_check_mark: | The name of the source tool                                  |
| `srcToolUrn`      | _string_                                                 | :heavy_check_mark: | The URN of the source tool                                   |
| `summarizer`      | _string_                                                 | :heavy_minus_sign: | The summarizer of the tool variation                         |
| `summary`         | _string_                                                 | :heavy_minus_sign: | The summary of the tool variation                            |
| `tags`            | _string_[]                                               | :heavy_minus_sign: | The tags of the tool variation                               |
| `title`           | _string_                                                 | :heavy_minus_sign: | Display name override for the tool                           |
