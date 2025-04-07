# CreateCompletionRequest

## Example Usage

```typescript
import { CreateCompletionRequest } from "@gram/sdk/models/components";

let value: CreateCompletionRequest = {
  model: "gpt-3.5-turbo-instruct",
  prompt: "Say this is a test",
};
```

## Fields

| Field                                     | Type                                      | Required                                  | Description                               | Example                                   |
| ----------------------------------------- | ----------------------------------------- | ----------------------------------------- | ----------------------------------------- | ----------------------------------------- |
| `maxTokens`                               | *number*                                  | :heavy_minus_sign:                        | The maximum number of tokens to generate. |                                           |
| `model`                                   | *string*                                  | :heavy_check_mark:                        | ID of the model to use.                   | gpt-3.5-turbo-instruct                    |
| `prompt`                                  | *string*                                  | :heavy_check_mark:                        | The prompt to generate completions for.   | Say this is a test                        |
| `temperature`                             | *number*                                  | :heavy_minus_sign:                        | Sampling temperature to use.              |                                           |