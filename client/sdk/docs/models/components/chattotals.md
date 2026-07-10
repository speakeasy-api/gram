# ChatTotals

Trace-entry counts across the entire returned generation, independent of pagination. Each message maps to exactly one entry: a message carrying tool calls counts as a tool call regardless of role, otherwise the role decides.

## Example Usage

```typescript
import { ChatTotals } from "@gram/client/models/components/chattotals.js";

let value: ChatTotals = {
  assistantMessages: 651773,
  riskOnly: 792446,
  toolCalls: 457844,
  toolResults: 976056,
  total: 24190,
  userMessages: 780242,
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `assistantMessages`                                                                                        | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Number of assistant messages (without tool calls) in the generation.                                       |
| `riskOnly`                                                                                                 | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Number of messages with an active (found, non-suppressed) risk finding in the generation.                  |
| `toolCalls`                                                                                                | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Number of messages carrying tool calls in the generation.                                                  |
| `toolResults`                                                                                              | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Number of tool-result messages in the generation.                                                          |
| `total`                                                                                                    | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Total trace entries in the generation (sum of the four entry-type counts; the `of N entries` denominator). |
| `userMessages`                                                                                             | *number*                                                                                                   | :heavy_check_mark:                                                                                         | Number of user messages in the generation.                                                                 |