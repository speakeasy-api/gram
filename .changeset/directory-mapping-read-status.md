---
"server": minor
---

Report whether each directory role mapping's source still exists, and list where each member's directory-mapped roles come from. Mappings now carry a `source_status` of `active` or `missing` (a deleted group, or an attribute value no live directory user has). Members now include their directory groups by stable ID, whether they have a live directory profile, and one entry per mapping that currently grants them a role, so a role reached through two groups shows both.
