#!/usr/bin/env bash

#MISE description="Start up the Gram Dashboard dev server"
#MISE hide=true

set -e

exec aube run --filter ./client/dashboard dev