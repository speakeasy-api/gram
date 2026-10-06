---
"server": minor
"dashboard": patch
---

Widgets and Explore now use the dashboard's date presets (15 minutes to 90 days), and Explore's Window control is the dashboard's date picker. Widgets saved with "24h" still open, as "1d". A page that places widgets uses the shared filter bar, configured to show only the catalog fields it names, and every widget on it answers within that bar:

- The page's date range replaces each widget's own window, and the buckets follow it.
- Page filters are ANDed with each widget's own filters, and a card names any page filter its dataset cannot apply.
- Dragging across a time chart narrows the page.

Open in Explore shows exactly what the card showed.
