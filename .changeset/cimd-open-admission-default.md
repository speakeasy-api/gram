---
"server": minor
"dashboard": minor
---

Default CIMD client admission to `open` for issuers that were never configured, instead of the internal `reporting` mode. No client that authenticates today stops authenticating: both modes admit every spec-valid client ID metadata document. What changes is that the resting policy is now a real, readable, operator-changeable value: new issuers are created as `open`, the settings page shows Open selected with its warning rather than nothing selected, and `presets` enforcement becomes something an operator opts into rather than a default anyone lands on.

Catalog-gap measurement survives the change. An open-mode admission still computes what `presets` would have decided and records it on `cimd.admission.decisions`, under the new `admitted_open_not_listed` outcome for a client no rule covers.

The custom client URL list now follows the unsaved selection in the admission mode field, so the URLs that `presets` enforces can be added before switching to it rather than after.
