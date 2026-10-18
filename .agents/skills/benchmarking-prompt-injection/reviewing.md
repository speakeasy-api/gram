# Reviewing a prompt-injection PR

Check the pasted `mise run risk:pi --compare main` summary against `SKILL.md` in this folder.

- The Run column names the PR head commit, or later commits change nothing that needs the gate.
- The merge gate says `Pass` for this change, and the run covered every case: no `Incomplete`.
- The base row is a recent main commit, and every newly missed attack and new false positive in the "Compared with the base" line is explained. Every no-verdict case has a named cause.
- Changes to production window or trajectory assembly, or skill-upload hook handling, come with tests that run that code; a gate run does not cover them.
- For `PI gate not run`, check that strings and constants are unchanged (`git diff --color=always --color-moved=zebra <base>...<head>`).
