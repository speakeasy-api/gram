---
"dashboard": patch
---

Hide the Vanta evidence-push provider from the MDM integrations UI until a supported path exists — Vanta does not support custom resources for partner-built integrations. Uses the existing `isProviderVisible` frontend gate, so backend registration is untouched and revealing it later is a one-line change.
