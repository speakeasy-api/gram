# ListToolExecutionLogsResult

Result of listing tool execution logs

## Example Usage

```typescript
import { ListToolExecutionLogsResult } from "@gram/client/models/components";

let value: ListToolExecutionLogsResult = {};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `logs`                                                                         | [components.ToolExecutionLog](../../models/components/toolexecutionlog.md)[]   | :heavy_minus_sign:                                                             | List of tool execution logs                                                    |
| `pagination`                                                                   | [components.PaginationResponse](../../models/components/paginationresponse.md) | :heavy_minus_sign:                                                             | Pagination metadata for list responses                                         |