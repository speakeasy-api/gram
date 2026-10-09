---
"server": patch
"dashboard": patch
---

Risk events found in a prompt attachment, such as a spreadsheet, now show their context. The risk-only transcript returned nothing for these findings because they belong to the attachment rather than a message; it now windows around the prompt the attachment was sent with, and the finding drawer shows the flagged attachment beneath that prompt. The session's risky-only count includes those prompts too.
