---
"dashboard": minor
---

The Windows device agent setup now has a "Download Intune package" button. It builds the three Win32 apps Intune needs (the identity config, the agent msi and the tray startup shortcut) as ready-to-upload `.intunewin` files, with a fresh agent token and the org's settings already filled in. A README in the bundle lists each app's install commands and detection rules, so admins no longer need IntuneWinAppUtil.
