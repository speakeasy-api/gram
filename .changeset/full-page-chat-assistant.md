---
"dashboard": minor
---

Add a full-page Project Assistant chat as a second way into the assistant, alongside the docked composer.

- New **Project Assistant** entry in the project sidebar (under Home) opening a `/chat` landing: a personalized "Ask your Project Assistant about your AI usage" composer with a cycling, crossfading placeholder, a `/` slash menu of starter prompts, your recent conversations grouped by time period ("Show all" to expand), and a set of enterprise-focused starter prompts.
- A `/chat/:id` conversation view with a back/new-chat bar and the conversation's title in the header, which updates live as the backend generates it.
- The project home page now embeds the same "Ask anything" widget.
- The dock and the full-page chat share **one** assistant runtime, so an in-flight conversation continues seamlessly when you expand the dock into the page (no lost response). The docked pill offers "Continue chat" to reopen a recent conversation, and pages that host their own chat runtime (Playground, Elements, assistant onboarding) hide the dock to avoid duplicate composers.
- A decorative rainbow "powder burst" header on the chat landing.
