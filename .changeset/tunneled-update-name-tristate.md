---
"server": minor
---

`tunneledMcp.updateServer` no longer requires `name`: like `allow_public` and `resource_identifier`, an omitted name leaves the stored value unchanged. Dashboard sections that edit a single field no longer send the cached display name alongside it, so saving one setting can never revert a rename that landed in between.
