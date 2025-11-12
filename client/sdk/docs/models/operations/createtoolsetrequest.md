# CreateToolsetRequest

## Example Usage

```typescript
import { CreateToolsetRequest } from "@gram/client/models/operations";

let value: CreateToolsetRequest = {
  createToolsetRequestBody: {
    name: "<value>",
  },
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `gramSession`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | Session header                                                                             |
| `gramKey`                                                                                  | *string*                                                                                   | :heavy_minus_sign:                                                                         | API Key header                                                                             |
| `gramProject`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | project header                                                                             |
| `createToolsetRequestBody`                                                                 | [components.CreateToolsetRequestBody](../../models/components/createtoolsetrequestbody.md) | :heavy_check_mark:                                                                         | N/A                                                                                        |