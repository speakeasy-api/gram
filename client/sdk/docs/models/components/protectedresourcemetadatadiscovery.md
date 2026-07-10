# ProtectedResourceMetadataDiscovery

Outcome of an RFC 9728 protected resource metadata probe against a remote MCP server. available=true exposes the parsed metadata; available=false exposes a typed unavailability reason. Always returned with HTTP 200 — probe failures (including 404 from upstream) are not errors at this layer because non-OAuth resource servers are an expected, normal outcome.

## Example Usage

```typescript
import { ProtectedResourceMetadataDiscovery } from "@gram/client/models/components/protectedresourcemetadatadiscovery.js";

let value: ProtectedResourceMetadataDiscovery = {
  available: false,
  discoveryWarnings: ["<value 1>", "<value 2>"],
};
```

## Fields

| Field               | Type                                                                                                               | Required           | Description                                                                                                                                                    |
| ------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `available`         | _boolean_                                                                                                          | :heavy_check_mark: | True when the upstream advertised an RFC 9728 document. False for any unavailability reason — see the unavailable field for the cause.                         |
| `discoveryWarnings` | _string_[]                                                                                                         | :heavy_check_mark: | Informational deviations from RFC 9728 detected on a successful probe (e.g. missing resource field, mismatched resource value). Empty when available is false. |
| `metadata`          | [components.ProtectedResourceMetadata](../../models/components/protectedresourcemetadata.md)                       | :heavy_minus_sign: | RFC 9728 OAuth Protected Resource Metadata advertised by a remote MCP server. Only fields the dashboard renders are typed; the RFC allows additional members.  |
| `unavailable`       | [components.ProtectedResourceMetadataUnavailable](../../models/components/protectedresourcemetadataunavailable.md) | :heavy_minus_sign: | Reason an RFC 9728 protected resource metadata probe was unavailable. Surfaced when available is false.                                                        |
