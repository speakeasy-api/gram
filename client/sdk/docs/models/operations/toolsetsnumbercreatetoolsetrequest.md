# ToolsetsNumberCreateToolsetRequest

## Example Usage

```typescript
import { ToolsetsNumberCreateToolsetRequest } from "@gram/sdk/models/operations";

let value: ToolsetsNumberCreateToolsetRequest = {
  gramSession: "Vitae exercitationem non aut.",
  gramProject: "Cupiditate sed et.",
  createToolsetRequestBody: {
    description: "Mollitia quisquam amet.",
    httpToolIds: [
      "Nostrum dolor eum dolores.",
      "Dolores ducimus cumque.",
      "A id in placeat quasi ut.",
    ],
    name: "Incidunt sed dolor ut.",
  },
};
```

## Fields

| Field                                                                                                                                                                                     | Type                                                                                                                                                                                      | Required                                                                                                                                                                                  | Description                                                                                                                                                                               | Example                                                                                                                                                                                   |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                                                                                                             | *string*                                                                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                                        | Session header                                                                                                                                                                            | Vitae exercitationem non aut.                                                                                                                                                             |
| `gramProject`                                                                                                                                                                             | *string*                                                                                                                                                                                  | :heavy_check_mark:                                                                                                                                                                        | project header                                                                                                                                                                            | Cupiditate sed et.                                                                                                                                                                        |
| `createToolsetRequestBody`                                                                                                                                                                | [components.CreateToolsetRequestBody](../../models/components/createtoolsetrequestbody.md)                                                                                                | :heavy_check_mark:                                                                                                                                                                        | N/A                                                                                                                                                                                       | {<br/>"description": "Mollitia quisquam amet.",<br/>"http_tool_ids": [<br/>"Nostrum dolor eum dolores.",<br/>"Dolores ducimus cumque.",<br/>"A id in placeat quasi ut."<br/>],<br/>"name": "Incidunt sed dolor ut."<br/>} |