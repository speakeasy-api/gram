# CreateToolsetRequest

## Example Usage

```typescript
import { CreateToolsetRequest } from "@gram/client/models/operations/createtoolset.js";

let value: CreateToolsetRequest = {
  createToolsetRequestBody: {
    name: "<value>",
  },
};
```

## Fields

| Field                      | Type                                                                                       | Required           | Description    |
| -------------------------- | ------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramSession`              | _string_                                                                                   | :heavy_minus_sign: | Session header |
| `gramKey`                  | _string_                                                                                   | :heavy_minus_sign: | API Key header |
| `gramProject`              | _string_                                                                                   | :heavy_minus_sign: | project header |
| `createToolsetRequestBody` | [components.CreateToolsetRequestBody](../../models/components/createtoolsetrequestbody.md) | :heavy_check_mark: | N/A            |
