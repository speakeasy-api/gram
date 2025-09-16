# Gram CLI

A command-line interface for interacting with Gram services and managing deployments.

## Local development

Quickstart:

```bash
$ go run . --help

NAME:
   gram - Remote MCP management

USAGE:
   gram [global options] command [command options]

VERSION:
   v0.1.0

COMMANDS:
   push     Push a deployment to Gram
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --help, -h     show help
   --version, -v  print the version
```

### Release updates

This is the typical workflow for releasing changes to the CLI.

1. Commit your changes via Pull Request.
   - Make sure to update [`version.go`](./version/version.go).
1. Check out the new commit locally.
1. Run `mise run cli:release`, and follow instructions to push the new tag.

Once the tag is pushed, your changes are available with:

```bash
$ go install github.com/speakeasy-api/gram/server/cmd/cli/gram@latest
```

Verify with:

```bash
$ go list -m -versions -json github.com/speakeasy-api/server/cmd/cli/gram@latest
```
