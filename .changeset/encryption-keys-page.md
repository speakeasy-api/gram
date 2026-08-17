---
"dashboard": minor
---

Add an Encryption Keys page where organization admins register the keys in
their own cloud KMS that Gram signs with. Keys list with their provider and
algorithm, and each has a detail page with a Verify action that proves Gram can
reach the key and sign with it, reporting what to fix when it cannot. Editing
covers the name, the backing credential, and the granted identity; the resource
name and algorithm are shown read-only, because a key record names one key
permanently. The page sits behind the same `customer_managed_encryption_keys`
entitlement as External Services.

External credential detail pages gain a KMS Keys tab listing the keys that
credential reaches, which is also where a refused credential delete explains
itself.
