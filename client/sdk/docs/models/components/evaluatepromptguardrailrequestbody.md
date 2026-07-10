# EvaluatePromptGuardrailRequestBody

## Example Usage

```typescript
import { EvaluatePromptGuardrailRequestBody } from "@gram/client/models/components/evaluatepromptguardrailrequestbody.js";

let value: EvaluatePromptGuardrailRequestBody = {
  chatId: "433452bb-8ee5-43e5-9055-b5386fa15e62",
  prompt: "<value>",
};
```

## Fields

| Field          | Type                                                                                 | Required           | Description                                                                                                                                                                  |
| -------------- | ------------------------------------------------------------------------------------ | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `chatId`       | _string_                                                                             | :heavy_check_mark: | The chat session to replay the guardrail against.                                                                                                                            |
| `messageTypes` | _string_[]                                                                           | :heavy_minus_sign: | Message types to judge (user_message, assistant_message, tool_request, tool_response), matching a policy's message_types. When empty or omitted, judges all supported types. |
| `modelConfig`  | [components.RiskPolicyModelConfig](../../models/components/riskpolicymodelconfig.md) | :heavy_minus_sign: | N/A                                                                                                                                                                          |
| `prompt`       | _string_                                                                             | :heavy_check_mark: | The guardrail prompt the LLM judge evaluates each in-scope message against.                                                                                                  |
| `scopeExempt`  | _string_                                                                             | :heavy_minus_sign: | CEL exemption predicate: the replay skips a message when this boolean expression is true. Omit/empty means no inline exemption.                                              |
| `scopeInclude` | _string_                                                                             | :heavy_minus_sign: | CEL scope predicate: the replay judges a message only when this boolean expression is true (in addition to message_types). Omit/empty means all messages are in scope.       |
