#!/usr/bin/env bash

#MISE dir="{{ config_root }}/server"
#MISE description="Warm the Go build cache for the server/worker/streams/admin daemons"
#MISE hide=true

#USAGE flag "--out <file>" help="Also write the cached executable to this path" default="/dev/null"

set -e

# `start:server`, `start:worker`, `start:streams` and `start:admin` are the SAME
# Go program -- `main.go` with a different subcommand -- launched concurrently
# by pitchfork. On a cold cache that means concurrent copies of one compile+link,
# each racing the others for cores: ~75s before any of them serves a request,
# and the boot cannot proceed to seeding until they do.
#
# Running the identical build once up front collapses that to a single compile,
# after which each `go run` is a cache hit. `zero` backgrounds this so it
# overlaps the Docker infra start and the migrations, which are IO-bound and
# leave the CPU idle -- so it is close to free in wall-clock terms.
#
# The build flags MUST match the start tasks: they feed the compilation and
# link cache keys, so any drift silently reintroduces cold builds.
# By default only warm the cache; local command wrappers can request a binary.
go build \
    -o "${usage_out:-/dev/null}" \
    ./main.go
