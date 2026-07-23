---
"server": minor
"dashboard": patch
---

Add `fetchMetadata` and `refreshMetadata` across all three remote identity provider tiers. `fetchMetadata` is keyed by issuer URL and persists nothing, as the pre-create step; `refreshMetadata` is keyed by issuer id and re-reads an existing provider's RFC 8414 document, persisting only discovered values (endpoints, the `*_supported` arrays, `client_id_metadata_document_supported`, and the documentation URLs) while leaving Gram's own behavior and display fields untouched. A "Refresh Discoverable Metadata" action is available from the Remote Identity Providers listing.
