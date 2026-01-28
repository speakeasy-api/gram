# ExternalMCPHeaderDefinition

## Example Usage

```typescript
import { ExternalMCPHeaderDefinition } from "@gram/client/models/components";

let value: ExternalMCPHeaderDefinition = {
  headerName: "<value>",
  name: "<value>",
  required: true,
  secret: false,
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `description`                                                  | *string*                                                       | :heavy_minus_sign:                                             | Description of the header                                      |
| `headerName`                                                   | *string*                                                       | :heavy_check_mark:                                             | The actual HTTP header name to send (e.g., X-Api-Key)          |
| `name`                                                         | *string*                                                       | :heavy_check_mark:                                             | The prefixed environment variable name (e.g., SLACK_X_API_KEY) |
| `placeholder`                                                  | *string*                                                       | :heavy_minus_sign:                                             | Placeholder value for the header                               |
| `required`                                                     | *boolean*                                                      | :heavy_check_mark:                                             | Whether the header is required                                 |
| `secret`                                                       | *boolean*                                                      | :heavy_check_mark:                                             | Whether the header value is secret                             |