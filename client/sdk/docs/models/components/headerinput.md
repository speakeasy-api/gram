# HeaderInput

Input for a remote MCP server header

## Example Usage

```typescript
import { HeaderInput } from "@gram/client/models/components/headerinput.js";

let value: HeaderInput = {
  name: "<value>",
};
```

## Fields

| Field                    | Type      | Required           | Description                                                                        |
| ------------------------ | --------- | ------------------ | ---------------------------------------------------------------------------------- |
| `description`            | _string_  | :heavy_minus_sign: | Description of the header                                                          |
| `isRequired`             | _boolean_ | :heavy_minus_sign: | Whether the header is required                                                     |
| `isSecret`               | _boolean_ | :heavy_minus_sign: | Whether the header value is a secret                                               |
| `name`                   | _string_  | :heavy_check_mark: | The header name                                                                    |
| `value`                  | _string_  | :heavy_minus_sign: | Static header value (mutually exclusive with value_from_request_header)            |
| `valueFromRequestHeader` | _string_  | :heavy_minus_sign: | Name of the inbound request header to pass through (mutually exclusive with value) |
