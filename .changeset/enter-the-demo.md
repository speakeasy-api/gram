---
"server": minor
"dashboard": minor
---

Add a self-serve path into the shared read-only demo organization. A new `auth.enterDemo` endpoint switches any authenticated session into the demo org (no membership required); request auth, grant resolution, and member/role listings gain demo carve-outs; the demo org always enforces a fixed read-only scope set with a verb-based write guard as backstop. The dashboard gains an `/explore-demo` entry route, an "explore a live demo org" link on the book-a-demo gate, and a demo banner whose exit switches back to the user's own organization without logging out.
