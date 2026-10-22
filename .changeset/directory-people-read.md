---
"server": minor
---

Store directory users' first and last names from Directory Sync events, and add a session-only `listDirectoryPeople` read that returns every live directory profile with its groups and whether it belongs to a member, has an account but is not a member, has not signed in, or shares an ambiguous email.
