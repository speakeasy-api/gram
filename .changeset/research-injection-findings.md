---
"server": minor
"dashboard": minor
---

The research agent now runs the prompt-injection judge over every page it fetches, and records a flagged page as a finding on the report. A vendor page that tries to steer whoever is reviewing the server says more about that server than any claim in the report, so the attempt is surfaced as evidence rather than only defended against: the agent still sees the page, labelled as material that tried to instruct it, and the finding is attached by the runner after extraction so a model that just read the manipulating page cannot leave it out. Pages the judge could not answer for are counted separately, because an empty findings list next to a judge outage does not mean nothing was tried.

Starting a research run also serializes properly: the check for an in-flight run and the insert that creates one now share a transaction behind a row lock, so two clicks that land together buy one run instead of two paid agent runs.
