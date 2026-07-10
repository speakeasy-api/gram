# UpdateTunneledMcpServerForm

Form for updating a tunneled MCP server source

## Example Usage

```typescript
import { UpdateTunneledMcpServerForm } from "@gram/client/models/components/updatetunneledmcpserverform.js";

let value: UpdateTunneledMcpServerForm = {
  id: "bce1dac9-30eb-46c9-874c-e44369bdc383",
  name: "<value>",
};
```

## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `id`                                                    | *string*                                                | :heavy_check_mark:                                      | The ID of the tunneled MCP server to update             |
| `name`                                                  | *string*                                                | :heavy_check_mark:                                      | Human-readable display name for the tunneled MCP server |