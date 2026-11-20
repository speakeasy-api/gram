---
"server": minor
---

The AI client catalog is now read from the database instead of the server binary. Servers load it before they start serving and refresh it every minute. A refresh that fails, comes back empty, or holds conflicting entries keeps the last good catalog. Preset lists now show entries by product id instead of the old fixed order. The catalog seed runs once more to link AI tool decisions saved since it first ran.
