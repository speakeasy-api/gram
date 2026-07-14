# UpdateServerForm

Form for updating a remote MCP server. When headers is provided, it represents the complete desired set of headers — any existing headers not in the list will be removed.

## Example Usage

```typescript
import { UpdateServerForm } from "@gram/client/models/components/updateserverform.js";

let value: UpdateServerForm = {
  id: "<id>",
};
```

## Fields

| Field           | Type                                                               | Required           | Description                                                                                                         |
| --------------- | ------------------------------------------------------------------ | ------------------ | ------------------------------------------------------------------------------------------------------------------- |
| `headers`       | [components.HeaderInput](../../models/components/headerinput.md)[] | :heavy_minus_sign: | The complete desired set of headers. Omit to leave headers unchanged. Provide an empty array to remove all headers. |
| `id`            | _string_                                                           | :heavy_check_mark: | The ID of the remote MCP server to update                                                                           |
| `name`          | _string_                                                           | :heavy_minus_sign: | Optional human-readable name. Pass an empty string to clear the existing name.                                      |
| `transportType` | _string_                                                           | :heavy_minus_sign: | The transport type for the remote MCP server                                                                        |
| `url`           | _string_                                                           | :heavy_minus_sign: | The URL of the remote MCP server                                                                                    |
