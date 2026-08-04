---
"dashboard": patch
---

Update the macOS setup walkthrough on the Device Agent page to install from
the signed `.pkg` (ADR-0015) instead of the retired curl-download-and-chmod
script. The pkg installs the daemon, CLI, menu-bar UI, and privileged
helper together and registers its own LaunchAgents, so the walkthrough now
covers a manual `installer` run or a normal MDM Package push instead of a
separate download/chmod/service-register sequence. Windows and Linux are
unaffected — they still ship as raw binaries.
