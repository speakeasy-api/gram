---
"server": minor
"dashboard": minor
---

Add `cli_destructive` risk-policy source for flagging destructive CLI commands.

Mirrors the existing `destructive_tool` shape (post-hoc batch scan, flag-only,
no live blocking) but is content-driven instead of annotation-driven. A
curated regex set covers shell (`rm -rf`, `dd`, `mkfs`, fork-bomb,
`chmod -R`, `chown -R`, `sudo <arg>`), git (`push --force`, `reset --hard`,
`clean -f`, `branch -D`), database (`DROP`, `TRUNCATE`, unguarded
`DELETE FROM`, `dropdb`), and cloud (`aws ec2 terminate-instances`,
`aws s3 rb`, `gcloud projects delete`, `kubectl delete ns/workloads`).

The scanner walks every recorded tool call's parsed arguments — no MCP
filter — so native Bash and `run_terminal_cmd` are now in scope alongside
MCP-routed calls whose arguments happen to carry destructive content.
First-match-wins iteration over map keys is sorted so rule_ids are
deterministic across runs.

PolicyCenter exposes the new source as a "Destructive CLI Commands" rule
category (category-toggle UX matching `destructive_tool`).
