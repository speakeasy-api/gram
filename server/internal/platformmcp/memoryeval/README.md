# Team memory eval

Synthetic corpus, questions and checks for team chat memory in the Platform MCP.

- **Corpus** (`Build`): 4 users, 150 chats over 60 days in the main project
  (25 decisions, 8 reversals, 22 timeline chats across 8 timelines, 10
  planted secrets, 3 private sessions) plus a 12-chat second project. Chat
  IDs are derived from chat keys.
- **Questions**: 55, each with an answer key. Answerable questions also name
  their source chats and the evidence terms a source must contain; the 5
  negative and 5 isolation questions expect "Not found" and have neither. The
  hard set has 7 temporal questions (what was true when another event
  happened) and 8 syntheses. Each needs three or more chats at least two
  weeks apart and shares no keywords with them, which
  `TestHardQuestionsShareNoKeywordsWithSources` enforces, so rewriting the
  question into a keyword search still leaves the agent to find and order
  several chats over time.
- **Mechanical checks** (CI, every PR): `TestSearchChatsBaselineMechanical`
  loads the corpus into Postgres and probes the `search_chats` baseline. It
  asserts no credential leaks, no other-project results and full retrieval of
  source chats, and logs the remaining leaks, private-session hits and
  estimated tool tokens. Arms C and D add their own probes and the recall
  size assertion.
- **Judged accuracy** (on demand, costs LLM calls): `mise run eval:memory`
  answers every question with each arm's tools, grades answers with a judge
  model that differs from the answering model, and prints a report. Set
  `OPENROUTER_DEV_KEY` first; `--runs` and `--out` are optional.

Arms: A has no memory tools, B is today's `search_chats` and `read_chat`.
Arms C (`search_team_memory`) and D (`recall_team_history` + `zoom` +
`search_team_memory`) are added by the PRs that ship those tools.

The flag turns on when arm D meets every gate:

- **Complete run**: no errored answers in either arm, since errors drop out
  of every rate below.
- **Hard-set lift**: at least 15 points above arm B on the hard set.
- **Overall accuracy**: at least 80%, and no lower than arm B.
- **Privacy**: no leaked secret, and nothing from another project or a
  private session.
- **Citations**: valid sources at least 95% of the time.
- **Negatives**: at least 4 of 5 answered "Not found".
- **Tokens**: recall block under 8k tokens, and tool tokens per question at
  most twice arm B's.
