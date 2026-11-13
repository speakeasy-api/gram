---
type: llm
focus: trace
weight: 1
---

The chart handles a withdraw that arrives while the CMS publish call is in flight so that a publish which still succeeds does not leave a withdrawn document published (for example by recording the request and unpublishing, or by compensating), and a test covers that sequence
