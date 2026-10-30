# How option D behaves: a case-by-case guide

This guide grows one question at a time. Each section answers one "how would X look?" question about option D. Option D is a declarative relationship schema evaluated over today's `principal_grants` rows. Each section shows the code that expresses the answer, then explains in plain terms what it does when a request arrives.

## How to read it

- **Implemented** means the code is on this branch and covered by tests. The section links the file and the test.
- **Sketched** means the code shows what option D *would* look like. It is not on the branch, or is on it only as far as the example needs. Sketched code may not compile as written.
- Every section ends with what it does *not* cover, and the cases where the answer would change.
- This guide describes option D. It doesn't compare D with other options or recommend it.

## Where the pieces are

| Piece | File | What it does |
| --- | --- | --- |
| Builders and compiler | `relschema/builders.go`, `relschema/schema.go` | `s.NewType`, `s.Grant`, `s.Permission` and so on. `Compile` rejects declarations that don't make sense. |
| Decoder | `relschema/decode.go` | Turns one stored `principal_grants` row into a relationship, or fails. |
| Evaluator | `relschema/eval.go` | Answers allowed, denied or undetermined for one permission on one object, with a trace. |
| Schemas | `mcp_server_schema.go` | The policy for one resource type, written with the builders. |
| Entry points | `mcp_server_access.go` | What services call (`RequireMCPServer`, `MCPServerCapabilities`). |
| Walkthroughs | `walkthroughs/` | A long trace of one request through all of the above. |

## Questions

_None answered yet._
