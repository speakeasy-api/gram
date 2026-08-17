#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Warm the Go build cache for the server/worker/streams daemons"
#MISE hide=true

set -e

# `start:server`, `start:worker` and `start:streams` are the SAME Go program --
# `main.go` with a different subcommand -- and pitchfork launches all three at
# once. On a cold cache that means three concurrent copies of one compile+link,
# each racing the others for cores: ~75s before any of them serves a request,
# and the boot cannot proceed to seeding until they do.
#
# Running the identical build once up front collapses that to a single compile,
# after which each `go run` is a cache hit. `zero` backgrounds this so it
# overlaps the Docker infra start and the migrations, which are IO-bound and
# leave the CPU idle -- so it is close to free in wall-clock terms.
#
# The ldflags MUST stay byte-identical to the three start tasks: they feed the
# link step's cache key, so any drift silently reintroduces the cold link.
# -o /dev/null because only the cache entry is wanted, not the binary.
GIT_SHA=$(git rev-parse HEAD)

go build \
    -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" \
    -o /dev/null \
    ./main.go
