---
"@gram-ai/elements": minor
---

Add two welcome/composer affordances:

- `WelcomeConfig.logo` — an optional logo image URL rendered above the title on the empty-thread welcome screen.
- A composer tool-mention picker button (next to the attachment button) that opens a list of the available tools and inserts an `@mention` for the chosen one — a discoverable counterpart to the existing type-`@` autocomplete. Hidden when tool mentions are disabled or there are no tools.
