# PromptGuardrailEvalResult

The result of replaying a prompt guardrail against one chat session. Read-only: no findings are persisted.

## Example Usage

```typescript
import { PromptGuardrailEvalResult } from "@gram/client/models/components/promptguardrailevalresult.js";

let value: PromptGuardrailEvalResult = {
  chatId: "3e612b8e-b098-421f-b061-a95ee29c28b2",
  flagged: false,
  judgedCount: 848488,
  totalCostUsd: 5018.46,
  totalLatencyMs: 45534,
  verdicts: [
    {
      completionTokens: 643057,
      confidence: 3547.37,
      costUsd: 9504.08,
      latencyMs: 666350,
      matched: false,
      messageId: "6367557b-d0b1-428e-b907-4b9c9ce354c1",
      messageType: "<value>",
      promptTokens: 750752,
      rationale: "<value>",
      seq: 667010,
      totalTokens: 550227,
    },
  ],
};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `chatId`                                                                                                       | *string*                                                                                                       | :heavy_check_mark:                                                                                             | The chat session that was replayed.                                                                            |
| `flagged`                                                                                                      | *boolean*                                                                                                      | :heavy_check_mark:                                                                                             | True when the guardrail flagged at least one in-scope message.                                                 |
| `judgedCount`                                                                                                  | *number*                                                                                                       | :heavy_check_mark:                                                                                             | Number of in-scope messages the judge evaluated.                                                               |
| `totalCostUsd`                                                                                                 | *number*                                                                                                       | :heavy_check_mark:                                                                                             | Total OpenRouter cost across in-scope judge calls, in USD.                                                     |
| `totalLatencyMs`                                                                                               | *number*                                                                                                       | :heavy_check_mark:                                                                                             | Aggregate judge latency overhead across in-scope messages, computed as the sum of per-message judge latencies. |
| `verdicts`                                                                                                     | [components.PromptGuardrailMessageVerdict](../../models/components/promptguardrailmessageverdict.md)[]         | :heavy_check_mark:                                                                                             | Per-message verdicts for in-scope messages, ordered by seq.                                                    |