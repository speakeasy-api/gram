#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Start up the Temporal worker"
#MISE sources=["server/**/*.go"]

GIT_SHA=$(git rev-parse HEAD)
go run -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X goa.design/clue/health.Version=${GIT_SHA}" main.go worker
