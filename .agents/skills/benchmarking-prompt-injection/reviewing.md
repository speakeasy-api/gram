# Reviewing a prompt-injection PR

First check whether the diff changes the prompt-injection detector's prompts or models. Only those changes require the paid benchmark. Otherwise, check targeted tests for the changed behavior; do not request a benchmark.

For prompt or model changes, check the pasted `mise run risk:pi --compare main` summary against `SKILL.md` in this folder.

- The Run column names the PR head commit, or later commits change nothing that needs the gate.
- The merge gate says `Pass` for this change, and the run covered every case: no `Incomplete`.
- The base row is a recent main commit, and every newly missed attack and new false positive in the "Compared with the base" line is explained. Every no-verdict case has a named cause.
- Changes to production window or trajectory assembly, or skill-upload hook handling, come with tests that run that code; a gate run does not cover them.
- For `PI gate not run`, verify that detector prompts and models are unchanged and that the stated targeted verification covers the change. Other string or constant changes do not require the benchmark.
