# VerifyURLResult

Outcome of a remote MCP server URL verification

## Example Usage

```typescript
import { VerifyURLResult } from "@gram/client/models/components/verifyurlresult.js";

let value: VerifyURLResult = {
  message: "<value>",
  verified: true,
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `httpStatus`                                                           | *number*                                                               | :heavy_minus_sign:                                                     | HTTP status code returned by the URL, if any                           |
| `message`                                                              | *string*                                                               | :heavy_check_mark:                                                     | Human-readable summary of the verification outcome                     |
| `verified`                                                             | *boolean*                                                              | :heavy_check_mark:                                                     | Whether the URL responded in a way consistent with a remote MCP server |