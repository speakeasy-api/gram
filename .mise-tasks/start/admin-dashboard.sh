#!/usr/bin/env bash

#MISE description="Start up the Speakeasy Admin dashboard dev server"
#MISE hide=true

set -e

exec aube run --filter ./client/admin dev
