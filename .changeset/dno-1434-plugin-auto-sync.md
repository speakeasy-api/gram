---
"server": patch
"dashboard": patch
---

Each plugin now has a "Sync changes automatically" switch, on by default, in the Create Plugin dialog and in the plugin's Settings tab. While it is off, changes to that plugin wait: the marketplace keeps the version last published until someone clicks Sync changes, which publishes them along with everything else. Other plugins keep syncing automatically. A plugin created with the switch off stays out of the marketplace until its first Sync changes. Auto sync can only be turned off once the plugin's changes have been synced, and the plugin's slug can't change while it is off. Turning it back on publishes the waiting changes. The marketplace card names the plugins whose auto sync is off, and edit messages say whether a change syncs on its own.

Plugin assignment changes that add or remove servers delivered through a role now republish the marketplace automatically, like every other plugin change.
