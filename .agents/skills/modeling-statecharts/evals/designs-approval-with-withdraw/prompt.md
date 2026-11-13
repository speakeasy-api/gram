---
tags: [skill-creator]
max_turns: 80
timeout_seconds: 3600
allowed_tools: [Read, Glob, Grep, Skill, Write, Edit, Bash]
---

I need a document approval flow built on our sc statechart package. An author submits a draft. Legal and security each approve or reject it, independently and in any order. A rejection from either sends it back to draft. Once both have approved we publish it by calling the CMS, which can fail and should be retried a couple of times. If review has not finished after 48 hours we send one escalation email and keep waiting. The author can withdraw the document at any point before it is published. Rules I care about: we never publish a document unless both legal and security approved that version; we never publish the same document twice; a withdrawn document is never left published. Model it first so we know the design holds, then build it and test it. I am not around to answer questions, so make reasonable assumptions and write them down.
