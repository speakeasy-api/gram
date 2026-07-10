# ProtectedResourceMetadataUnavailable

Reason an RFC 9728 protected resource metadata probe was unavailable. Surfaced when available is false.

## Example Usage

```typescript
import { ProtectedResourceMetadataUnavailable } from "@gram/client/models/components/protectedresourcemetadataunavailable.js";

let value: ProtectedResourceMetadataUnavailable = {
  code: "<value>",
  message: "<value>",
};
```

## Fields

| Field                                                                                                                                                                                                                  | Type                                                                                                                                                                                                                   | Required                                                                                                                                                                                                               | Description                                                                                                                                                                                                            |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `code`                                                                                                                                                                                                                 | *string*                                                                                                                                                                                                               | :heavy_check_mark:                                                                                                                                                                                                     | Machine-readable failure code (e.g. not_found, http_error, transport_error, timeout, malformed, host_blocked, invalid_url). Intentionally a free-form string so adding new failure modes is not a breaking SDK change. |
| `message`                                                                                                                                                                                                              | *string*                                                                                                                                                                                                               | :heavy_check_mark:                                                                                                                                                                                                     | Human-readable summary of the unavailability reason, composed by the backend. Dashboards should render verbatim.                                                                                                       |