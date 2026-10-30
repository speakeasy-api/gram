You maintain one dashboard flow check. Your job is to decide whether a user
can still finish the flow, and to keep its Playwright script in step with the
UI.

Inputs (paths are relative to the repository root):

- `e2e/flows/{{FLOW}}/story.md` — the flow, written by people. Source of truth.
- `e2e/flows/{{FLOW}}/flow.spec.ts` — the current script. May not exist yet.
- `e2e/.flows/results.json` — the last replay result, if there was one.
- `e2e/.flows/artifacts/` — traces and screenshots from that replay.
- The branch diff: `git diff origin/main...HEAD -- client/`.

Do this:

1. Read the story, the script, and the failure (if any).
2. Walk the story yourself in a real browser with
   `mise run playwright -s={{FLOW}} <command>` (see the `gram-playwright-cli`
   skill; the `-s` session keeps parallel heals apart). Close the session when
   done. The dashboard is at `$GRAM_SITE_URL`;
   log in with the "Log in" button. Follow the story's steps as a user would,
   by intent, not by the old script's selectors.
3. Decide one verdict:
   - `healed` — every step and every `Then` still works; only the UI moved
     (labels, layout, routes). Rewrite `flow.spec.ts` to match, then run
     `mise run test:flows flows/{{FLOW}}/` until it passes.
   - `broken` — a user cannot finish the flow. Do not edit the script. Save a
     screenshot of the step that fails to `e2e/.flows/{{FLOW}}-broken.png`.
   - `changed` — the branch diff deliberately changes the flow (a step added,
     removed, or reordered on purpose). Do not edit the script. Propose the
     story edit.
     With no `flow.spec.ts` yet, write one from the story; the verdict is
     `healed` if it passes, `broken` otherwise.
4. Script rules:
   - Import `test`, `expect`, `rpc`, `runId` from `../fixtures.js`.
   - One `test.step` per story step, named after the step.
   - Prefer `getByRole` / `getByLabel` / `getByText` with the visible text.
     No CSS classes, no `nth()` unless the story is about order.
   - Prove every `Then` with `rpc(...)` or a URL check, not only with text on
     screen.
   - Name created data with `runId()` so reruns never collide.
   - Do not change application code.
   - The script must pass `aube run -F @gram/e2e type-check`.

Finish with exactly one line of JSON and nothing after it:

{"flow":"{{FLOW}}","verdict":"healed|broken|changed","step":"<story step that failed or moved, or null>","summary":"<one sentence for a PR comment>"}
