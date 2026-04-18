#!/usr/bin/env bash

#MISE description="Start the mock OIDC provider for local development auth"

set -e

exec go run ./mock-oidc/main --config ./mock-oidc/mock-oidc.example.yaml
