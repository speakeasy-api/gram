# RemoteMcpServerHeader

A header configured for a remote MCP server

## Example Usage

```typescript
import { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader.js";

let value: RemoteMcpServerHeader = {
  createdAt: new Date("2026-08-14T23:30:20.301Z"),
  id: "a66f1596-7be4-4861-91c6-0dc4207a1ade",
  isRequired: true,
  isSecret: true,
  name: "<value>",
  updatedAt: new Date("2026-02-03T06:03:46.687Z"),
};
```

## Fields

| Field                    | Type                                                                                          | Required           | Description                                        |
| ------------------------ | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------- |
| `createdAt`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the header was created                        |
| `description`            | _string_                                                                                      | :heavy_minus_sign: | Description of the header                          |
| `id`                     | _string_                                                                                      | :heavy_check_mark: | The ID of the header                               |
| `isRequired`             | _boolean_                                                                                     | :heavy_check_mark: | Whether the header is required                     |
| `isSecret`               | _boolean_                                                                                     | :heavy_check_mark: | Whether the header value is a secret               |
| `name`                   | _string_                                                                                      | :heavy_check_mark: | The header name                                    |
| `updatedAt`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the header was last updated                   |
| `value`                  | _string_                                                                                      | :heavy_minus_sign: | The header value (redacted if secret)              |
| `valueFromRequestHeader` | _string_                                                                                      | :heavy_minus_sign: | Name of the inbound request header to pass through |
