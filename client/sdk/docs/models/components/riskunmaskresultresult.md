# RiskUnmaskResultResult

## Example Usage

```typescript
import { RiskUnmaskResultResult } from "@gram/client/models/components/riskunmaskresultresult.js";

let value: RiskUnmaskResultResult = {
  id: "70b611f7-c150-4188-af24-69ab1a07f51b",
  match: "<value>",
};
```

## Fields

| Field   | Type     | Required           | Description                                                                                                                                       |
| ------- | -------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`    | _string_ | :heavy_check_mark: | The risk result ID.                                                                                                                               |
| `match` | _string_ | :heavy_check_mark: | The plaintext matched secret or sensitive data for this result. Empty string when the finding has no top-level match (e.g. a spans-only finding). |
