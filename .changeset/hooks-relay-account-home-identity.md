---
"server": patch
---

When an AI tool runs hooks with `HOME` pointed at a temporary folder, the hooks binary now finds the device agent under the account's real home and asks the running agent before any machine-wide copy. It no longer runs an outdated machine-wide agent that could open a stack of macOS "Keychain Not Found" dialogs.
