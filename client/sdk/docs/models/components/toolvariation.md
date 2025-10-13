# ToolVariation

## Example Usage

```typescript
import { ToolVariation } from "@gram/client/models/components";

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

| Field                                          | Type                                           | Required                                       | Description                                    |
| ---------------------------------------------- | ---------------------------------------------- | ---------------------------------------------- | ---------------------------------------------- |
| `confirm`                                      | *string*                                       | :heavy_minus_sign:                             | The confirmation mode for the tool variation   |
| `confirmPrompt`                                | *string*                                       | :heavy_minus_sign:                             | The confirmation prompt for the tool variation |
| `createdAt`                                    | *string*                                       | :heavy_check_mark:                             | The creation date of the tool variation        |
| `description`                                  | *string*                                       | :heavy_minus_sign:                             | The description of the tool variation          |
| `groupId`                                      | *string*                                       | :heavy_check_mark:                             | The ID of the tool variation group             |
| `id`                                           | *string*                                       | :heavy_check_mark:                             | The ID of the tool variation                   |
| `name`                                         | *string*                                       | :heavy_minus_sign:                             | The name of the tool variation                 |
| `srcToolName`                                  | *string*                                       | :heavy_check_mark:                             | The name of the source tool                    |
| `srcToolUrn`                                   | *string*                                       | :heavy_check_mark:                             | The URN of the source tool                     |
| `summarizer`                                   | *string*                                       | :heavy_minus_sign:                             | The summarizer of the tool variation           |
| `updatedAt`                                    | *string*                                       | :heavy_check_mark:                             | The last update date of the tool variation     |