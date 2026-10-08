# Reviewing a prompt-injection PR

Read with `SKILL.md` in this folder, which says what the author must paste.

- The pasted commit is the PR head, or later commits change no "Yes" row.
- `model`, `refusal_fallback_model` (`anthropic/claude-opus-4.8` today) and `prefilter_threshold` match the code, and each run has its own metrics.
- For changes to production window or trajectory assembly, the PR shows unit tests or windowed fixtures, not only a gate run.
- Every run is shown with the summary lines for both modes, at most one rerun per version, every false positive is answered by a code change or a relabel, and every `fail_open` has a named cause.
- The confirmation hash changes only with `SystemPrompt` or `WindowInstructions`, the questions hash only with `PrefilterQuestions`, the corpus hash only with fixtures or the loader. Compare each with the last PR that ran the gate (`gh pr list --state merged --search '"merge gate run" in:body' --limit 1`; until one exists, compare with a smoke check on main). With a key, the smoke check writes the current prompt hashes for about $0.01; never compare its corpus hash, which covers only the smoke cases.
- For `PI gate not run: <reason>`, check that the diff changes no "Yes" row; for a move, check that strings and constants are byte-identical (`git diff --color=always --color-moved=zebra`; without `--color=always`, piped output shows no moves).
