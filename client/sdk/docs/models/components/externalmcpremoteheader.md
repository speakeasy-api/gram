# ExternalMCPRemoteHeader

A header requirement for a remote MCP server

## Example Usage

```typescript
import { ExternalMCPRemoteHeader } from "@gram/client/models/components/externalmcpremoteheader.js";

let value: ExternalMCPRemoteHeader = {
  name: "<value>",
};
```

## Fields

| Field         | Type      | Required           | Description                                           |
| ------------- | --------- | ------------------ | ----------------------------------------------------- |
| `description` | _string_  | :heavy_minus_sign: | Description of the header                             |
| `isRequired`  | _boolean_ | :heavy_minus_sign: | Whether this header is required                       |
| `isSecret`    | _boolean_ | :heavy_minus_sign: | Whether this header value should be treated as secret |
| `name`        | _string_  | :heavy_check_mark: | Header name                                           |
| `placeholder` | _string_  | :heavy_minus_sign: | Placeholder value to show when collecting this header |
