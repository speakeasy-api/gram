---
"server": minor
---

Hooks bootstrap scripts now report a failed hooks binary download or checksum mismatch to Speakeasy. The report is best effort, uses a three second timeout, and lands as a hooks diagnostic row plus a `hooks.install_failure` metric.
