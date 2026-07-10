# ListRiskResultsForAgentResult

## Example Usage

```typescript
import { ListRiskResultsForAgentResult } from "@gram/client/models/components/listriskresultsforagentresult.js";

let value: ListRiskResultsForAgentResult = {
  results: [
    {
      chatMessageId: "083f3043-25ed-4137-93cb-aa7b60a4e8d3",
      createdAt: new Date("2024-07-09T15:18:45.835Z"),
      id: "6a896655-7d52-4668-ba16-b9f10db2f958",
      matchRedacted: "<value>",
      policyId: "36579f33-d698-4095-8a69-6b7c004c77ed",
      policyVersion: 855873,
      positionKnown: false,
      source: "<value>",
    },
  ],
  totalCount: 48669,
};
```

## Fields

| Field        | Type                                                                             | Required           | Description                                                                  |
| ------------ | -------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------- |
| `nextCursor` | _string_                                                                         | :heavy_minus_sign: | Cursor for the next page of results.                                         |
| `results`    | [components.RiskResultRedacted](../../models/components/riskresultredacted.md)[] | :heavy_check_mark: | The list of risk results with match content redacted to opaque fingerprints. |
| `totalCount` | _number_                                                                         | :heavy_check_mark: | Total number of findings across all enabled policies.                        |
