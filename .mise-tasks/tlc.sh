#!/usr/bin/env bash

#MISE description="Run the TLC model checker for TLA+ specs; arguments are passed to TLC"
#MISE dir="{{ cwd }}"
#MISE quiet=true

set -euo pipefail

jar="$(mise where http:tla2tools)/tla2tools.jar"

exec java -XX:+UseParallelGC -cp "$jar" tlc2.TLC "$@"
