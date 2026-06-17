#!/usr/bin/env bash

#MISE dir="{{ config_root }}/dev-idp"
#MISE description="Generate from the dev-idp Goa design files (top-level dev-idp/ project)"

set -e

# `go tool goa` builds the goa binary on demand and `goa gen` then `go run`s
# its throwaway main; every one of those builds stamps VCS info, which has
# crashed CI with `signal: bus error` while shelling out to git. Codegen
# binaries are throwaway and never shipped, so the VCS stamp buys nothing.
export GOFLAGS="-buildvcs=false ${GOFLAGS:-}"

# `goa gen` writes a throwaway `goa<random>/main.go` generator into its working
# directory. goa removes it on both success and failure, but on a hard interrupt
# (Ctrl-C) or --debug it can linger. That main imports goa's codegen deps
# (kin-openapi, mohae/deepcopy, ...), so a later `go mod tidy` walks the orphaned
# package and writes spurious go.sum entries that CI then flags as dirty
# (AGE-2621); dev-idp shares the root go.mod, so it has the same exposure. The
# pre-run sweep clears any stale scratch from a crashed prior run; the trap removes
# it on exit while preserving goa's exit code.
#
# Invoke goa via `go tool` so codegen always uses the version pinned by the
# `tool` directive in go.mod rather than whichever `goa` happens to be on PATH;
# version drift would otherwise show up as unexpected generated-output diffs.
cleanup() {
  rc=$?
  rm -rf goa*
  exit "$rc"
}
trap cleanup EXIT

rm -rf goa*
go tool goa gen github.com/speakeasy-api/gram/dev-idp/design -o .
