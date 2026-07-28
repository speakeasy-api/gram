# HookMCPAttributionEntry

Transcript-derived MCP attribution for one model API request. Claude redacts user-configured MCP server names to 'custom' on its OTEL telemetry, but records the real names in the local session transcript; hooks ship them here so ingest can restore the redacted names.


## Fields

| Field                                                                             | Type                                                                              | Required                                                                          | Description                                                                       |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| `McpServer`                                                                       | `*string`                                                                         | :heavy_minus_sign:                                                                | Unredacted MCP server name from the transcript.                                   |
| `McpTool`                                                                         | `*string`                                                                         | :heavy_minus_sign:                                                                | Unredacted MCP tool name from the transcript.                                     |
| `RequestID`                                                                       | `string`                                                                          | :heavy_check_mark:                                                                | Provider API request identifier (e.g. Claude's req_*) the attribution applies to. |