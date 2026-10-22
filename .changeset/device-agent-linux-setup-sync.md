---
"dashboard": patch
---

The Device Agent Linux steps now match the Linux install guide. The service step adds the systemd user unit to `default.target`, so the agent starts again after a reboot or logout. Before, it never came back. The step also turns on self-update for single machines through `local.json` and suggests lingering for machines nobody logs in to. The verify step points to the guide for the optional tray icon. The fleet identity step asks for a `managed.json` the user can read (`0644`, or `0640` with the user's group), and its troubleshooting note says to restart the agent after changing the file.
