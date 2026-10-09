#!/usr/bin/env bash

#MISE description="Start up the Speakeasy Dashboard dev server"
#MISE hide=true

set -e

exec aube run --filter ./client/dashboard dev