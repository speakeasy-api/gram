---
"server": minor
"dashboard": minor
---

Add user-configurable custom destructive CLI patterns to `cli_destructive` risk policies.

Policies using the `cli_destructive` source can now include a list of custom
regex patterns (label + Go RE2 regex) that are evaluated against tool call
arguments in addition to the built-in curated set. Patterns are stored as JSONB
in the `risk_policies` table, validated at write time (max 50 entries, valid
RE2, label ≤100 chars), and passed through the Temporal drain workflow to the
batch scanner.

The PolicyCenter form exposes an expandable panel when the "Destructive CLI
Commands" category is selected, allowing users to add, edit, and remove custom
patterns with live preview of the built-in set.
