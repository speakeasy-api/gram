# Offline MCP registry record contract

Authority: MCP registry OpenAPI at revision
`bf4e88cbe8d1a635c06144ccea1d24cb52fa6186`:
https://github.com/modelcontextprotocol/registry/blob/bf4e88cbe8d1a635c06144ccea1d24cb52fa6186/docs/reference/api/openapi.yaml

Source YAML SHA-256: `3ed9ca429754087f2c7de81aa845c1cab3286c33941ac48482f923bbd6f1442d`.

`record.schema.json` contains `ServerResponse` and only its transitively reachable
component schemas. Offline conversion preserves their keywords and annotations,
rewrites local `#/components/schemas/` references to `#/$defs/`, and declares JSON
Schema draft 2020-12. No runtime fetch or publication-policy inference is used.
To update, fetch the pinned YAML, select the response's reachable definitions,
apply those transformations, review the diff and run the contract tests.

Go is authoritative: `jsonschema/v6` with `AssertFormat` uses native `uri` and
`date-time` validation, without custom adapters. The current library accepts a
mid-month leap second and rejects IPvFuture URI literals. These are adopted
validator boundaries, not claims that upstream or the RFC mandates either result.
Unknown metadata extensions remain open; root official metadata is explicitly
closed by upstream. Namespace placement is not an additional ownership check.

Gram rules live in record/mutation validation: exact-case name identity, payload
limits, immutable identity and ordered remote endpoints. Stored invalid data stays
readable and repairable within those invariants; this schema does not rewrite it.
Validation preserves extension JSON and numeric precision rather than projecting
records through Go structs.

Run `mise exec -- go test ./server/internal/mcpregistry/contract`.
`contract-cases.json` covers record rules and native format boundaries. The two
unchanged starter records are local/test fixtures only; their manifest pins record
bytes, not schema authority. No production import is part of this contract.
