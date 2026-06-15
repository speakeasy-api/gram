---
"dashboard": patch
---

Fix the Cmd+K command palette's "Recently Visited" list showing an assistant's opaque id (e.g. "Assistant · 0190abcd") instead of its name. Visits are recorded centrally from the URL, which for the id-keyed assistant detail route fell back to the id. The assistant detail page now registers its name as the recents label, and `App` consults that override (re-recording when the name resolves asynchronously), so the palette shows the assistant name.
