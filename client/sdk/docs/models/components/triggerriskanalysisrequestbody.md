# TriggerRiskAnalysisRequestBody

## Example Usage

```typescript
import { TriggerRiskAnalysisRequestBody } from "@gram/client/models/components/triggerriskanalysisrequestbody.js";

let value: TriggerRiskAnalysisRequestBody = {
  id: "12dda5ba-ede8-4de3-a00a-3bf6890d524a",
};
```

## Fields

| Field   | Type     | Required           | Description                                                                                                                                                            |
| ------- | -------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`    | _string_ | :heavy_check_mark: | The policy ID.                                                                                                                                                         |
| `limit` | _number_ | :heavy_minus_sign: | Cap the backfill at the most recent N unanalyzed messages. Defaults to 100 (the recent-N drain budget). Pass 0 to request a full backfill of every unanalyzed message. |
