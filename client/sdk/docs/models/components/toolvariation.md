# ToolVariation

## Example Usage

```typescript
import { ToolVariation } from "@gram/client/models/components/toolvariation.js";

let value: ToolVariation = {
  createdAt: "1715329601419",
  groupId: "<id>",
  id: "<id>",
  srcToolName: "<value>",
  srcToolUrn: "<value>",
  updatedAt: "1735622567982",
};
```

## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `confirm`                                                    | *string*                                                     | :heavy_minus_sign:                                           | The confirmation mode for the tool variation                 |
| `confirmPrompt`                                              | *string*                                                     | :heavy_minus_sign:                                           | The confirmation prompt for the tool variation               |
| `createdAt`                                                  | *string*                                                     | :heavy_check_mark:                                           | The creation date of the tool variation                      |
| `description`                                                | *string*                                                     | :heavy_minus_sign:                                           | The description of the tool variation                        |
| `destructiveHint`                                            | *boolean*                                                    | :heavy_minus_sign:                                           | Override: if true, the tool may perform destructive updates  |
| `groupId`                                                    | *string*                                                     | :heavy_check_mark:                                           | The ID of the tool variation group                           |
| `id`                                                         | *string*                                                     | :heavy_check_mark:                                           | The ID of the tool variation                                 |
| `idempotentHint`                                             | *boolean*                                                    | :heavy_minus_sign:                                           | Override: if true, repeated calls have no additional effect  |
| `name`                                                       | *string*                                                     | :heavy_minus_sign:                                           | The name of the tool variation                               |
| `openWorldHint`                                              | *boolean*                                                    | :heavy_minus_sign:                                           | Override: if true, the tool interacts with external entities |
| `readOnlyHint`                                               | *boolean*                                                    | :heavy_minus_sign:                                           | Override: if true, the tool does not modify its environment  |
| `srcToolName`                                                | *string*                                                     | :heavy_check_mark:                                           | The name of the source tool                                  |
| `srcToolUrn`                                                 | *string*                                                     | :heavy_check_mark:                                           | The URN of the source tool                                   |
| `summarizer`                                                 | *string*                                                     | :heavy_minus_sign:                                           | The summarizer of the tool variation                         |
| `tags`                                                       | *string*[]                                                   | :heavy_minus_sign:                                           | The tags of the tool variation                               |
| `title`                                                      | *string*                                                     | :heavy_minus_sign:                                           | Display name override for the tool                           |
| `updatedAt`                                                  | *string*                                                     | :heavy_check_mark:                                           | The last update date of the tool variation                   |