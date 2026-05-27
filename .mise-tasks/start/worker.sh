#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Start up the Temporal worker"

GIT_SHA=$(git rev-parse HEAD)
RUNTIME_IMAGE_HASH=$(mise run hash:assistant-runtime-image)
go run -ldflags="-X github.com/speakeasy-api/gram/server/cmd/gram.GitSHA=${GIT_SHA} -X github.com/speakeasy-api/gram/server/cmd/gram.AssistantRuntimeImageHash=${RUNTIME_IMAGE_HASH} -X goa.design/clue/health.Version=${GIT_SHA}" main.go worker
