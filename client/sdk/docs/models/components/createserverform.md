# CreateServerForm

Form for creating a new remote MCP server

## Example Usage

```typescript
import { CreateServerForm } from "@gram/client/models/components/createserverform.js";

let value: CreateServerForm = {
  headers: [],
  transportType: "<value>",
  url: "https://tattered-understanding.name",
};
```

## Fields

| Field           | Type                                                               | Required           | Description                                                                              |
| --------------- | ------------------------------------------------------------------ | ------------------ | ---------------------------------------------------------------------------------------- |
| `headers`       | [components.HeaderInput](../../models/components/headerinput.md)[] | :heavy_check_mark: | Headers to send when proxying requests to the remote server                              |
| `name`          | _string_                                                           | :heavy_minus_sign: | Optional human-readable name for the remote MCP server. Empty values are stored as null. |
| `transportType` | _string_                                                           | :heavy_check_mark: | The transport type for the remote MCP server (e.g. streamable-http)                      |
| `url`           | _string_                                                           | :heavy_check_mark: | The URL of the remote MCP server                                                         |
