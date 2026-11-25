#!/usr/bin/env bash

#MISE description="Type-check repository tooling and the source SDK with the root tsconfig"

set -e

exec aube exec tsc --noEmit
