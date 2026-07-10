# GetToolUsageFilterOptionsPayload

Payload for target-aware MCP and tool usage filter options

## Example Usage

```typescript
import { GetToolUsageFilterOptionsPayload } from "@gram/client/models/components/gettoolusagefilteroptionspayload.js";

let value: GetToolUsageFilterOptionsPayload = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Start time in ISO 8601 format                                                                 | 2025-12-19T10:00:00Z                                                                          |
| `optionTypes`                                                                                 | [components.OptionTypes](../../models/components/optiontypes.md)[]                            | :heavy_minus_sign:                                                                            | Filter option types to include. Empty means all option types.                                 |                                                                                               |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | End time in ISO 8601 format                                                                   | 2025-12-19T11:00:00Z                                                                          |