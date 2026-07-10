# SearchToolCallsFilter

Filter criteria for searching tool calls

## Example Usage

```typescript
import { SearchToolCallsFilter } from "@gram/client/models/components/searchtoolcallsfilter.js";

let value: SearchToolCallsFilter = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field          | Type                                                                                          | Required           | Description                                                        | Example              |
| -------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------ | -------------------- |
| `deploymentId` | _string_                                                                                      | :heavy_minus_sign: | Deployment ID filter                                               |                      |
| `eventSource`  | _string_                                                                                      | :heavy_minus_sign: | Event source filter (e.g., 'hook', 'tool_call', 'chat_completion') |                      |
| `from`         | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')       | 2025-12-19T10:00:00Z |
| `functionId`   | _string_                                                                                      | :heavy_minus_sign: | Function ID filter                                                 |                      |
| `gramUrn`      | _string_                                                                                      | :heavy_minus_sign: | Gram URN filter (single URN, use gram_urns for multiple)           |                      |
| `to`           | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')         | 2025-12-19T11:00:00Z |
