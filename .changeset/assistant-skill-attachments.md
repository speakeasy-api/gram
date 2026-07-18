---
"server": minor
---

Add assistant skill attachments to the management API and assistant read model. Skill distribution mutations now target exactly one plugin or assistant, with plugin and assistant target fields optional on mutation responses; plugin-only distribution lists retain required plugin fields. Assistants expose resolved skill references, and skill detail responses report active assistant usage.
