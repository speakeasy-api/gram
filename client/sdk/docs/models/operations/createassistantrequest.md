# CreateAssistantRequest

## Example Usage

```typescript
import { CreateAssistantRequest } from "@gram/client/models/operations/createassistant.js";

let value: CreateAssistantRequest = {
  createAssistantForm: {
    instructions: "<value>",
    model: "Ranchero",
    name: "<value>",
    toolsets: [],
  },
};
```

## Fields

| Field                 | Type                                                                             | Required           | Description    |
| --------------------- | -------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`         | _string_                                                                         | :heavy_minus_sign: | Session header |
| `gramProject`         | _string_                                                                         | :heavy_minus_sign: | project header |
| `createAssistantForm` | [components.CreateAssistantForm](../../models/components/createassistantform.md) | :heavy_check_mark: | N/A            |
