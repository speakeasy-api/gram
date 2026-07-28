# VerifyURLForm

Form for probing a remote MCP server URL

## Example Usage

```typescript
import { VerifyURLForm } from "@gram/client/models/components/verifyurlform.js";

let value: VerifyURLForm = {
  transportType: "<value>",
  url: "https://miserly-trolley.org/",
};
```

## Fields

| Field           | Type     | Required           | Description                                                         |
| --------------- | -------- | ------------------ | ------------------------------------------------------------------- |
| `transportType` | _string_ | :heavy_check_mark: | The transport type for the remote MCP server (e.g. streamable-http) |
| `url`           | _string_ | :heavy_check_mark: | The URL of the remote MCP server to probe                           |
