---
"server": patch
---

Add an internal staff-only Reset to production action in Admin and the local catalog CLI. Reset previews exact safe changes and structural conflicts, requires destination-specific confirmation, and restores production-visible catalog content while retaining unpublished records, installed connections, and credentials. Operation-bound plans reject stale or concurrent changes and support safe receipt replay without allowing refresh confirmations to authorize resets. Reset affects only the selected non-production destination, not existing independent preview databases.
