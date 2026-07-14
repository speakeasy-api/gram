---
"server": minor
---

Distribute observability hooks through a pinned, checksum-verified Go binary bootstrapper. Organizations can choose an install-failure policy: a new "Allow on Install Failure" setting lets hook events pass when the binary can't be installed on a developer machine, instead of the default fail-closed behavior. Observability mode keeps its guarantee that no hook can delay or block the agent, including during first-time binary installation.
