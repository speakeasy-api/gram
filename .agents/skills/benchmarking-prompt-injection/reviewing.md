# Reviewing a prompt-injection PR

Check the pasted `mise run risk:pi` summary against `SKILL.md` in this folder.

- The Run column names the PR head commit, without `+ uncommitted`, or later commits change nothing that needs the gate.
- The merge gate says `Pass` for this change, and the run covered every case: no `Incomplete`.
- The model line matches the code: `PrefilterModel`, `ConfirmationModel`, the `anthropic/claude-opus-4.8` fallback and `PrefilterThreshold`. A run without the fallback does not count.
- The confirmation prompt hash changes only with `SystemPrompt` or `WindowInstructions`, and the questions hash only with `PrefilterQuestions`; compare them with main's line.
- Every newly missed attack and new false positive in the "Compared with main" line is explained, and every no-verdict case has a named cause.
- Changes to production window or trajectory assembly, or skill-upload hook handling, come with tests that run that code; a gate run does not cover them.
- For `PI gate not run`, check that strings and constants are unchanged (`git diff --color=always --color-moved=zebra <base>...<head>`).
