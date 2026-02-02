---
"@gram-ai/elements": patch
---

Fix tool mentions not working inside Shadow DOM. The composer's tool mention autocomplete used `document.querySelector` to find the textarea, which can't reach elements inside a shadow root. Changed to use `getRootNode()` so it correctly queries within the Shadow DOM when present.
