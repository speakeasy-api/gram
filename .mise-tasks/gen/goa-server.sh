#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Generate from Goa design files"

set -e

# `goa gen` writes a throwaway `goa<random>/main.go` generator into its working
# directory. goa removes it on both success and failure, but on a hard interrupt
# (Ctrl-C) or --debug it can linger. That main imports goa's codegen deps
# (kin-openapi, mohae/deepcopy, ...), so a later `go mod tidy` walks the orphaned
# package and writes spurious go.sum entries that CI then flags as dirty
# (AGE-2621). The pre-run sweep clears any stale scratch from a crashed prior run;
# the trap removes it on exit while preserving goa's exit code.
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
go tool goa gen github.com/speakeasy-api/gram/server/design
