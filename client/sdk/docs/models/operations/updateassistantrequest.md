# UpdateAssistantRequest

## Example Usage

```typescript
import { UpdateAssistantRequest } from "@gram/client/models/operations/updateassistant.js";

let value: UpdateAssistantRequest = {
  updateAssistantForm: {
    id: "58f643a4-6d69-4c52-821f-261ddbdec1ae",
  },
};
```

## Fields

| Field                 | Type                                                                             | Required           | Description    |
| --------------------- | -------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`         | _string_                                                                         | :heavy_minus_sign: | Session header |
| `gramProject`         | _string_                                                                         | :heavy_minus_sign: | project header |
| `updateAssistantForm` | [components.UpdateAssistantForm](../../models/components/updateassistantform.md) | :heavy_check_mark: | N/A            |
