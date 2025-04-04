# ToolsetsNumberCreateToolsetRequest

## Example Usage

```typescript
import { ToolsetsNumberCreateToolsetRequest } from "@gram/sdk/models/operations";

let value: ToolsetsNumberCreateToolsetRequest = {
  createToolsetRequestBody: {
    name: "<value>",
  },
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `gramSession`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | Session header                                                                             |
| `gramProject`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | project header                                                                             |
| `createToolsetRequestBody`                                                                 | [components.CreateToolsetRequestBody](../../models/components/createtoolsetrequestbody.md) | :heavy_check_mark:                                                                         | N/A                                                                                        |