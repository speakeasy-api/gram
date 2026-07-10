# GetHooksSummaryPayload

Payload for getting aggregated hooks metrics

## Example Usage

```typescript
import { GetHooksSummaryPayload } from "@gram/client/models/components/gethookssummarypayload.js";

let value: GetHooksSummaryPayload = {
  filters: [
    {
      path: "@user.region",
    },
  ],
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
  typesToInclude: [
    "mcp",
    "skill",
  ],
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `filters`                                                                                     | [components.LogFilter](../../models/components/logfilter.md)[]                                | :heavy_minus_sign:                                                                            | Filter conditions (same as listHooksTraces)                                                   |                                                                                               |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Start time in ISO 8601 format                                                                 | 2025-12-19T10:00:00Z                                                                          |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | End time in ISO 8601 format                                                                   | 2025-12-19T11:00:00Z                                                                          |
| `typesToInclude`                                                                              | [components.TypesToInclude](../../models/components/typestoinclude.md)[]                      | :heavy_minus_sign:                                                                            | Hook types to include (mcp, local, skill). If empty, includes all types.                      | [<br/>"mcp",<br/>"skill"<br/>]                                                                |