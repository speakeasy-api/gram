---
"dashboard": patch
---

Redesign the app shell as a sidebar-only vertical split. The full-width top nav bar is removed and all of its functionality moves into the sidebar: a combined org/project workspace switcher and the logo in the header, and the account menu in the footer (inline theme toggle, Docs/Changelog/Get Support, and a new **Roadmap** link replacing the old bug/feature link). Light mode is now the default, the brand gradient line sits beneath the main-panel header, and pages gain a `Page.Header.Actions` toolbar slot. Switching projects now shows a nav skeleton instead of flashing empty while permissions reload.
