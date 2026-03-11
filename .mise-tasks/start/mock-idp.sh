#!/usr/bin/env bash
#MISE description="Start the mock Speakeasy IDP server for local development auth"

set -e

exec go run ./mock-speakeasy-idp/main
