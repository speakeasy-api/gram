# ChatNumberCompletionRequest

## Example Usage

```typescript
import { ChatNumberCompletionRequest } from "@gram/sdk/models/operations";

let value: ChatNumberCompletionRequest = {
  createCompletionRequest: {
    model: "gpt-3.5-turbo-instruct",
    prompt: "Say this is a test",
  },
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `gramSession`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | Session header                                                                           |
| `gramProject`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | project header                                                                           |
| `createCompletionRequest`                                                                | [components.CreateCompletionRequest](../../models/components/createcompletionrequest.md) | :heavy_check_mark:                                                                       | N/A                                                                                      |