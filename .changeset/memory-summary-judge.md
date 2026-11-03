---
"server": minor
---

Masked chat summaries for Organizational Memory are now written by the chat analysis pipeline instead of a separate 15-minute sweep. They run for organizations with Organizational Memory turned on (the Business Memory Extraction toggle in the staff admin dashboard), after a chat has been quiet for 30 minutes, and again each time a chat resumes and goes quiet. Fact extraction now reads masked transcripts and masks the facts it stores.
