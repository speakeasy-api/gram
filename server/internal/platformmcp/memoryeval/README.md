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
  estimated tool tokens. Arms C, D and E have their own probes, and arm D's
  also checks that the default recall block stays inside its 4k-token budget.
- **Judged accuracy** (on demand, costs LLM calls): `mise run eval:memory`
  answers every question with each arm's tools, grades answers with a judge
  model that differs from the answering model, and prints a report. Set
  `OPENROUTER_DEV_KEY` first; `--runs`, `--models` and `--out` are optional.
  `--models` takes a comma separated list of answering models, and the report
  has one row per model and arm with accuracy, tokens, cost and latency, so a
  run can compare models as well as arms.
  Before the arms run, `bench.Prepare` writes every team chat's masked summary
  and the day, week and month digests with `--summary-model` and
  `--digest-model` (defaults: the live jobs' models), and the report shows
  what that cost, so summary and digest models can be compared too. Existing
  summaries and digests for the target are replaced on each run, so never point
  it at a database whose memory you need to keep.
- **Runner** (`bench`): arms, the agent loop, judging and scoring live in a
  plain package that takes a corpus already loaded into a database, so the
  tests and future scheduled runs share one implementation.

Arms: A has no memory tools, B is today's `search_chats` and `read_chat`.
Arm C is `search_team_memory` alone. Arm D is `recall_team_history` + `zoom` +
`search_team_memory`; it recalls as of the corpus anchor (`WithClock`), so a
corpus built at a fixed date recalls the same history whenever it runs, and
its sweep reads the largest recall block plus everything reachable by zooming
down from it. Admin eval runs score arms A to D. Arm E is
`search_team_memory` with hybrid retrieval: the same masked items are embedded
in memory (`--embedding-model`, default the business memory model) and each
query fuses the vector ranks with the full-text ranks (reciprocal rank
fusion). It needs no schema change. Its results on this synthetic corpus
inform, rather than settle, whether team memory needs embeddings. The judged report adds a retrieval recall table for arms C
and E, querying with evidence words and with each question's own text, so
retrieval can be compared without the answering agent.

Planned rollout criterion, once arm D lands: Organizational Memory is enabled for
customers (the staff admin Business Memory Extraction toggle) only when arm D meets every
gate. Nothing in this package evaluates the gates yet; read them off the report.

- **Complete run**: no errored answers in either arm, since errors drop out
  of the graded rates below (accuracy, citations, negatives) while still
  adding to tool tokens per question.
- **Hard-set lift**: at least 15 points above arm B on the hard set.
- **Overall accuracy**: at least 80%, and no lower than arm B.
- **Privacy**: no leaked secret, and nothing from another project or a
  private session.
- **Citations**: valid sources at least 95% of the time.
- **Negatives**: at least 4 of 5 answered "Not found".
- **Tokens**: recall block under 8k tokens, and tool tokens per question at
  most twice arm B's.
