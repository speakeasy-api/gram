# GetObservabilityOverviewPayload

Payload for getting observability overview metrics

## Example Usage

```typescript
import { GetObservabilityOverviewPayload } from "@gram/client/models/components/getobservabilityoverviewpayload.js";

let value: GetObservabilityOverviewPayload = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field               | Type                                                                                          | Required           | Description                                                                                            | Example              |
| ------------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------ | -------------------- |
| `accountType`       | _string_                                                                                      | :heavy_minus_sign: | Optional account type filter ('team' or 'personal')                                                    |                      |
| `apiKeyId`          | _string_                                                                                      | :heavy_minus_sign: | Optional API key ID filter                                                                             |                      |
| `eventSource`       | _string_                                                                                      | :heavy_minus_sign: | Optional event source filter (e.g. 'hook')                                                             |                      |
| `externalOrgId`     | _string_                                                                                      | :heavy_minus_sign: | Optional filter to a single AI account by its provider org id; scopes the overview to that one account |                      |
| `externalUserId`    | _string_                                                                                      | :heavy_minus_sign: | Optional external user ID filter                                                                       |                      |
| `from`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | Start time in ISO 8601 format                                                                          | 2025-12-19T10:00:00Z |
| `hookSource`        | _string_                                                                                      | :heavy_minus_sign: | Optional hook source filter (e.g. 'cursor', 'claude-code')                                             |                      |
| `includeTimeSeries` | _boolean_                                                                                     | :heavy_minus_sign: | Whether to include time series data (default: true)                                                    |                      |
| `mcpServerId`       | _string_                                                                                      | :heavy_minus_sign: | Optional MCP server ID filter (fronting server; spans both remote-backed and toolset-backed activity)  |                      |
| `remoteMcpServerId` | _string_                                                                                      | :heavy_minus_sign: | Optional Remote MCP server ID filter                                                                   |                      |
| `to`                | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | End time in ISO 8601 format                                                                            | 2025-12-19T11:00:00Z |
| `toolsetSlug`       | _string_                                                                                      | :heavy_minus_sign: | Optional toolset/MCP server slug filter                                                                |                      |
| `userId`            | _string_                                                                                      | :heavy_minus_sign: | Optional internal user ID filter                                                                       |                      |
