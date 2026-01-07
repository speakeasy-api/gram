# UpsertGlobalToolVariationForm

## Example Usage

```typescript
import { UpsertGlobalToolVariationForm } from "@gram/client/models/components";

let value: UpsertGlobalToolVariationForm = {
  srcToolName: "<value>",
  srcToolUrn: "<value>",
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `confirm`                                                | [components.Confirm](../../models/components/confirm.md) | :heavy_minus_sign:                                       | The confirmation mode for the tool variation             |
| `confirmPrompt`                                          | *string*                                                 | :heavy_minus_sign:                                       | The confirmation prompt for the tool variation           |
| `description`                                            | *string*                                                 | :heavy_minus_sign:                                       | The description of the tool variation                    |
| `name`                                                   | *string*                                                 | :heavy_minus_sign:                                       | The name of the tool variation                           |
| `srcToolName`                                            | *string*                                                 | :heavy_check_mark:                                       | The name of the source tool                              |
| `srcToolUrn`                                             | *string*                                                 | :heavy_check_mark:                                       | The URN of the source tool                               |
| `summarizer`                                             | *string*                                                 | :heavy_minus_sign:                                       | The summarizer of the tool variation                     |
| `summary`                                                | *string*                                                 | :heavy_minus_sign:                                       | The summary of the tool variation                        |
| `tags`                                                   | *string*[]                                               | :heavy_minus_sign:                                       | The tags of the tool variation                           |