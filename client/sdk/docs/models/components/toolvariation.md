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

| Field             | Type       | Required           | Description                                                  |
| ----------------- | ---------- | ------------------ | ------------------------------------------------------------ |
| `confirm`         | _string_   | :heavy_minus_sign: | The confirmation mode for the tool variation                 |
| `confirmPrompt`   | _string_   | :heavy_minus_sign: | The confirmation prompt for the tool variation               |
| `createdAt`       | _string_   | :heavy_check_mark: | The creation date of the tool variation                      |
| `description`     | _string_   | :heavy_minus_sign: | The description of the tool variation                        |
| `destructiveHint` | _boolean_  | :heavy_minus_sign: | Override: if true, the tool may perform destructive updates  |
| `groupId`         | _string_   | :heavy_check_mark: | The ID of the tool variation group                           |
| `id`              | _string_   | :heavy_check_mark: | The ID of the tool variation                                 |
| `idempotentHint`  | _boolean_  | :heavy_minus_sign: | Override: if true, repeated calls have no additional effect  |
| `name`            | _string_   | :heavy_minus_sign: | The name of the tool variation                               |
| `openWorldHint`   | _boolean_  | :heavy_minus_sign: | Override: if true, the tool interacts with external entities |
| `readOnlyHint`    | _boolean_  | :heavy_minus_sign: | Override: if true, the tool does not modify its environment  |
| `srcToolName`     | _string_   | :heavy_check_mark: | The name of the source tool                                  |
| `srcToolUrn`      | _string_   | :heavy_check_mark: | The URN of the source tool                                   |
| `summarizer`      | _string_   | :heavy_minus_sign: | The summarizer of the tool variation                         |
| `tags`            | _string_[] | :heavy_minus_sign: | The tags of the tool variation                               |
| `title`           | _string_   | :heavy_minus_sign: | Display name override for the tool                           |
| `updatedAt`       | _string_   | :heavy_check_mark: | The last update date of the tool variation                   |
