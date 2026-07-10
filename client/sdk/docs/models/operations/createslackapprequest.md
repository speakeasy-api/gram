# CreateSlackAppRequest

## Example Usage

```typescript
import { CreateSlackAppRequest } from "@gram/client/models/operations";

let value: CreateSlackAppRequest = {
  createSlackAppRequestBody: {
    name: "<value>",
    toolsetIds: [],
  },
};
```

## Fields

| Field                       | Type                                                                                         | Required           | Description    |
| --------------------------- | -------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`               | _string_                                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`               | _string_                                                                                     | :heavy_minus_sign: | project header |
| `createSlackAppRequestBody` | [components.CreateSlackAppRequestBody](../../models/components/createslackapprequestbody.md) | :heavy_check_mark: | N/A            |
