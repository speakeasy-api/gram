---
name: writing-skills
description: Use when adding, editing, or reviewing an agent skill in this repo (.agents/skills/), when new guidance needs to reach every supported harness (.claude, .codex, .opencode, .cursor), or when deciding whether repeated agent failures should become a skill. Triggers: "add a skill", "write a skill", "SKILL.md", "skills:sync".
---

# Writing repo skills

Skills are agent-facing reference docs in `.agents/skills/<name>/SKILL.md`, symlinked into each supported harness's dotfolder. Core principle: a skill is only as good as a cold agent's ability to act on it — validate with fresh-context subagents performing the real workflow, not by rereading it yourself.

## Mechanics

1. Baseline per the eval loop below, then author `.agents/skills/<name>/SKILL.md` — kebab-case, verb-first name (`writing-skills`, not `skill-creation`).
2. Run `mise run skills:sync` — creates the symlinks in `.claude/skills/`, `.codex/skills/`, `.opencode/skills/`, `.cursor/skills/` and prunes stale ones. Never hand-create the links; you will miss a harness.
3. Do not add a manual skills table to `CLAUDE.md`; harnesses discover skills from the synced directories and each skill's frontmatter.
4. Commit the skill and the symlinks together.

## Frontmatter

`name` + `description` only. The description is triggering conditions ONLY — third person, starts "Use when …", packed with the symptoms, synonyms, commands, and error strings an agent would match on. Never summarize the skill's workflow there: agents follow the summary and skip the body.

## Body shape

Succinct reference, not narrative: overview with the core principle → quick-reference table → per-workflow detail → common mistakes. Target well under 1000 words. One excellent example beats many. Make only claims you have verified by running the commands, and record observed failure modes with exact error text — hypothetical caveats age badly, empirical ones survive review.

## Eval loop (required)

Baseline first: watch an agent attempt the task WITHOUT the skill and record the failure verbatim. No baseline failure ⇒ no skill needed.

1. Write the minimal skill addressing those observed failures.
2. Dispatch a fresh-context subagent with no prior knowledge. It must perform a REAL instance of the workflow using only the skill — consulting fallback docs counts as a documentation failure — and return structured feedback: VERDICT (ship/no-ship), RATING /10, ANSWERS, CONFUSIONS quoting the text at fault, SUGGESTED EDITS.
3. Fold the feedback in; repeat with a new subagent each round until one returns ship-as-is (9+/10) with no load-bearing confusions.

Vary rounds between do-the-task and adversarial trap questions; live environments surface what desk-checking cannot. Edits after the ship verdict also get a round — one-line additions included.

## External (recommended) skills

Third-party skills are not committed. Declare them in `.agents/recommended-skills.json` — `repo` + `ref` (absolute commit SHA) + `path` (subdirectory to extract) — and let the tooling own the rest: `./zero` asks once and persists `USE_RECOMMENDED_SKILLS` to `mise.local.toml`; when true, `mise run skills:sync` installs/updates the set into `.agents/skills/<name>/` and keeps every installed path out of git via `.git/info/exclude`. To add one: manifest entry, then `mise run skills:recommended` (`--yes` skips the prompt) for the initial install; thereafter `mise run skills:sync` keeps the set updated. To bump: change the SHA, re-run sync.

## Common mistakes

- Hand-symlinking one harness dir and missing the other three — `mise skills:sync` owns the links.
- Description that summarizes the procedure — agents act on it and never read the body.
- Caveats invented at the desk instead of failures observed in a run — evaluators refute them.
- Leaning on plugin or private docs — this repo is public and other harnesses lack them; skills must be self-contained.
