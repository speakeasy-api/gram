#!/usr/bin/env bash
#MISE description="Generate SDK from OpenAPI spec"
#MISE sources=["server/gen/http/openapi3.yaml", "overlays/goa.yaml"]
#MISE outputs=["client/sdk/**/*"]

set -e
exec speakeasy run
