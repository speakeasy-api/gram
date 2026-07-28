# ListToolsRequest

## Example Usage

```typescript
import { ListToolsRequest } from "@gram/client/models/operations/listtools.js";

let value: ListToolsRequest = {};
```

## Fields

| Field          | Type                                                           | Required           | Description                                                                                              |
| -------------- | -------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------- |
| `cursor`       | _string_                                                       | :heavy_minus_sign: | The cursor to fetch results from                                                                         |
| `limit`        | _number_                                                       | :heavy_minus_sign: | The number of tools to return per page                                                                   |
| `deploymentId` | _string_                                                       | :heavy_minus_sign: | The deployment ID. If unset, latest deployment will be used.                                             |
| `urnPrefix`    | _string_                                                       | :heavy_minus_sign: | Filter tools by URN prefix (e.g. 'tools:http:kitchen-sink' to match all tools starting with that prefix) |
| `toolTypes`    | [operations.ToolTypes](../../models/operations/tooltypes.md)[] | :heavy_minus_sign: | N/A                                                                                                      |
| `gramSession`  | _string_                                                       | :heavy_minus_sign: | Session header                                                                                           |
| `gramProject`  | _string_                                                       | :heavy_minus_sign: | project header                                                                                           |
