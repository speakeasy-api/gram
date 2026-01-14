---
"dashboard": patch
---

Updated the dashboard and vite config so that monaco editor and various three.js dependencies are not included in the main app bundle. This was causing extreme bloat of that bundle which ultimately slows down loading times of the web app.
