# Test helpers

## YAML golden snapshots

Use `RequireYAMLGolden` to snapshot captured JSON as readable YAML:

```go
testenv.RequireYAMLGolden(t, "testdata/request.golden.yaml", capturedJSON)
```

Pass actual output from the boundary being tested, such as an HTTP request body.
The helper accepts any fixture path, relative to the test package or absolute.
Use the `.golden.yaml` suffix so repository formatters leave generated output alone.

Normal runs compare against committed fixtures without writing them; missing files
and mismatches fail. Regenerate deliberately with the test binary's `-update` flag:

```sh
mise run test:server ./internal/<package>/ -args -update
```

Review and commit the generated diff, then rerun without `-update`. The generator
emits captured data only, including multiline strings. Keep explanations in docs
or test setup rather than editing the generated fixtures.

Comparison ignores object-key order and YAML formatting, preserves exact number
literals and string contents, and requires one document on each side. YAML keys
must be strings; numeric scalars use JSON syntax. Duplicate keys and aliases are
rejected. Parallel tests sharing a fixture are serialized during updates and must
produce the same JSON value.
