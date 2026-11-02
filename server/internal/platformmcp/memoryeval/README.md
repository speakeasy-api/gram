# Team memory eval

Synthetic corpus, questions and checks for team chat memory in the Platform MCP.

- **Corpus** (`Build`): 4 users, 150 chats over 60 days in the main project
  (25 decisions, 8 reversals, 22 timeline chats across 8 timelines, 10
  planted secrets, 3 private sessions) plus a 12-chat second project. Chat
  IDs are derived from chat keys.
- **Questions**: 40, each with an answer key. Answerable questions also name
  their source chats and the evidence terms a source must contain; the 5
  negative and 5 isolation questions expect "Not found" and have neither.
- **Mechanical checks** (CI, every PR): `TestSearchChatsBaselineMechanical`
  loads the corpus into Postgres and probes the `search_chats` baseline. It
  asserts no credential leaks, no other-project results and full retrieval of
  source chats, and logs the remaining leaks, private-session hits and
  estimated tool tokens. Arms C and D add their own probes and the recall
  size assertion.
- **Judged accuracy** (on demand, costs LLM calls): `mise run eval:memory`
  answers every question with each arm's tools, grades answers with a judge
  model that differs from the answering model, and prints a report. Set
  `OPENROUTER_DEV_KEY` first; `--runs`, `--models` and `--out` are optional.
  `--models` takes a comma separated list of answering models, and the report
  has one row per model and arm with accuracy, tokens, cost and latency, so a
  run can compare models as well as arms.
- **Runner** (`bench`): arms, the agent loop, judging and scoring live in a
  plain package that takes a corpus already loaded into a database, so the
  tests and future scheduled runs share one implementation.

Arms: A has no memory tools, B is today's `search_chats` and `read_chat`.
Arms C (`search_team_memory`) and D (`recall_team_history` + `zoom` +
`search_team_memory`) are added by the PRs that ship those tools.

The flag turns on when arm D reaches at least 80% accuracy and beats arm B by
15 points, cites valid sources at least 95% of the time, leaks no secret,
returns nothing from another project or a private session, answers at least
4 of 5 negative questions "Not found", and keeps its recall block under 8k
tokens.
