# ExternalMCPHeaderDefinition

## Example Usage

```typescript
import { ExternalMCPHeaderDefinition } from "@gram/client/models/components/externalmcpheaderdefinition.js";

let value: ExternalMCPHeaderDefinition = {
  headerName: "<value>",
  name: "<value>",
  required: true,
  secret: false,
};
```

## Fields

| Field         | Type      | Required           | Description                                                    |
| ------------- | --------- | ------------------ | -------------------------------------------------------------- |
| `description` | _string_  | :heavy_minus_sign: | Description of the header                                      |
| `headerName`  | _string_  | :heavy_check_mark: | The actual HTTP header name to send (e.g., X-Api-Key)          |
| `name`        | _string_  | :heavy_check_mark: | The prefixed environment variable name (e.g., SLACK_X_API_KEY) |
| `placeholder` | _string_  | :heavy_minus_sign: | Placeholder value for the header                               |
| `required`    | _boolean_ | :heavy_check_mark: | Whether the header is required                                 |
| `secret`      | _boolean_ | :heavy_check_mark: | Whether the header value is secret                             |
