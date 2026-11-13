---
type: llm
focus: trace
weight: 1
---

The chart handles an abort that arrives while the create-database call is in flight so that a database which still gets created is deleted, and a test covers that sequence
