# KickoffAssistantMessageRequest

## Example Usage

```typescript
import { KickoffAssistantMessageRequest } from "@gram/client/models/operations";

let value: KickoffAssistantMessageRequest = {
  kickoffMessageRequestBody: {
    assistantId: "719c1753-7a06-45bd-a9b1-87f759a127ea",
    correlationId: "<id>",
  },
};
```

## Fields

| Field                       | Type                                                                                         | Required           | Description    |
| --------------------------- | -------------------------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramSession`               | _string_                                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`               | _string_                                                                                     | :heavy_minus_sign: | project header |
| `kickoffMessageRequestBody` | [components.KickoffMessageRequestBody](../../models/components/kickoffmessagerequestbody.md) | :heavy_check_mark: | N/A            |
