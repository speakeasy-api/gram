# Plugin generation

- Copilot CLI and VS Code share discovery locations, but use distinct wire and response codecs.
- Generate explicit provider names: `copilot-cli` or `vscode-copilot`, never the ambiguous `copilot` runtime name.
- Register each lifecycle once. Copilot loads overlapping hook files from the same discovery directories.
- Bump `hooksGeneratorVersion` for every generated-hooks behavior change.
- Keep bootstrap checksum verification, atomic installation, quoting, and exit-code propagation intact.
- Never log generated credentials or copy them into fixtures. Canonical ingest behavior lives in `server/internal/hooks/`.
