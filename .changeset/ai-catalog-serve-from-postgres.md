---
"server": minor
---

The AI client catalog is now read from the database instead of the server binary. Servers load it before they start serving and refresh it every minute. If the database catalog is empty or invalid, servers use the built-in catalog until a valid one loads, and a refresh never replaces a good catalog with a bad one. CIMD preset lists now show entries by product id. The catalog seed runs once more to link AI tool decisions saved since it first ran.
