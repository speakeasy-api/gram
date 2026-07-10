# ListHooksTracesResult

Result of listing hook traces

## Example Usage

```typescript
import { ListHooksTracesResult } from "@gram/client/models/components/listhookstracesresult.js";

let value: ListHooksTracesResult = {
  traces: [],
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `nextCursor`                                                                 | *string*                                                                     | :heavy_minus_sign:                                                           | Cursor for next page                                                         |
| `traces`                                                                     | [components.HookTraceSummary](../../models/components/hooktracesummary.md)[] | :heavy_check_mark:                                                           | List of hook trace summaries                                                 |