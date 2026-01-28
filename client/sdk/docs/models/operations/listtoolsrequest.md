# ListToolsRequest

## Example Usage

```typescript
import { ListToolsRequest } from "@gram/client/models/operations";

let value: ListToolsRequest = {};
```

## Fields

| Field                                                                                                     | Type                                                                                                      | Required                                                                                                  | Description                                                                                               |
| --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `cursor`                                                                                                  | *string*                                                                                                  | :heavy_minus_sign:                                                                                        | The cursor to fetch results from                                                                          |
| `limit`                                                                                                   | *number*                                                                                                  | :heavy_minus_sign:                                                                                        | The number of tools to return per page                                                                    |
| `deploymentId`                                                                                            | *string*                                                                                                  | :heavy_minus_sign:                                                                                        | The deployment ID. If unset, latest deployment will be used.                                              |
| `sourceSlug`                                                                                              | *string*                                                                                                  | :heavy_minus_sign:                                                                                        | Filter tools by source slug (e.g. 'kitchen-sink' to get tools with URN prefix 'tools:http:kitchen-sink:') |
| `gramSession`                                                                                             | *string*                                                                                                  | :heavy_minus_sign:                                                                                        | Session header                                                                                            |
| `gramProject`                                                                                             | *string*                                                                                                  | :heavy_minus_sign:                                                                                        | project header                                                                                            |