# ListToolUsageTracesPayload

Payload for listing target-aware MCP and tool usage traces

## Example Usage

```typescript
import { ListToolUsageTracesPayload } from "@gram/client/models/components/listtoolusagetracespayload.js";

let value: ListToolUsageTracesPayload = {
  filters: [
    {
      path: "@user.region",
    },
  ],
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field                | Type                                                                                                                   | Required           | Description                                                                                                                                              | Example              |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------- |
| `accountType`        | _string_                                                                                                               | :heavy_minus_sign: | Optional account type filter ('team' or 'personal'). 'team' includes unclassified traces.                                                                |                      |
| `cursor`             | _string_                                                                                                               | :heavy_minus_sign: | Cursor for pagination                                                                                                                                    |                      |
| `filters`            | [components.LogFilter](../../models/components/logfilter.md)[]                                                         | :heavy_minus_sign: | Arbitrary attribute filter conditions from the af URL param                                                                                              |                      |
| `from`               | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                          | :heavy_check_mark: | Start time in ISO 8601 format                                                                                                                            | 2025-12-19T10:00:00Z |
| `hookSources`        | _string_[]                                                                                                             | :heavy_minus_sign: | Hook plugin sources to include. Direct hosted MCP calls have no hook source and are excluded when this filter is set.                                    |                      |
| `hostedToolsetSlugs` | _string_[]                                                                                                             | :heavy_minus_sign: | Hosted MCP toolset slugs to include                                                                                                                      |                      |
| `limit`              | _number_                                                                                                               | :heavy_minus_sign: | Number of traces to return                                                                                                                               |                      |
| `query`              | _string_                                                                                                               | :heavy_minus_sign: | Free-text attribute search string from the q URL param. Matches useful identifier attributes such as Gram URN, conversation ID, and trigger instance ID. |                      |
| `shadowServerNames`  | _string_[]                                                                                                             | :heavy_minus_sign: | Shadow MCP server names to include                                                                                                                       |                      |
| `sort`               | [components.ListToolUsageTracesPayloadSort](../../models/components/listtoolusagetracespayloadsort.md)                 | :heavy_minus_sign: | Sort order                                                                                                                                               |                      |
| `targetTypes`        | [components.ListToolUsageTracesPayloadTargetTypes](../../models/components/listtoolusagetracespayloadtargettypes.md)[] | :heavy_minus_sign: | Target types to include. Empty means all target types.                                                                                                   |                      |
| `to`                 | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date)                          | :heavy_check_mark: | End time in ISO 8601 format                                                                                                                              | 2025-12-19T11:00:00Z |
| `userFilters`        | [components.ToolUsageUserFilter](../../models/components/toolusageuserfilter.md)[]                                     | :heavy_minus_sign: | Typed user identities to include                                                                                                                         |                      |
