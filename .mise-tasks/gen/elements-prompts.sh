#!/usr/bin/env bash

#MISE description="Generate server constants from Elements prompt sources"

set -e

mise exec -- go generate ./server/internal/assistants
