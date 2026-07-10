# UpdateSlackAppRequest

## Example Usage

```typescript
import { UpdateSlackAppRequest } from "@gram/client/models/operations";

let value: UpdateSlackAppRequest = {
  updateSlackAppRequestBody: {
    id: "74ee1e72-69d0-4f9c-af04-3aa9fdeb14c5",
  },
};
```

## Fields

| Field                       | Type                                                                                         | Required           | Description    |
| --------------------------- | -------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`               | _string_                                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`               | _string_                                                                                     | :heavy_minus_sign: | project header |
| `updateSlackAppRequestBody` | [components.UpdateSlackAppRequestBody](../../models/components/updateslackapprequestbody.md) | :heavy_check_mark: | N/A            |
