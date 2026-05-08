#!/usr/bin/env bash

#MISE description="Open a thing with the system default application"
#MISE quiet=true
#MISE hide=true

#USAGE arg "<thing>" help="The thing to open (e.g. a file path or URL)"

set -e

thing="${usage_thing}"

if [[ -n "${WSL_DISTRO_NAME:-}" ]]; then
  if command -v wslview &>/dev/null; then
    wslview "$thing"
  else
    powershell.exe -c "Start-Process \"$thing\""
  fi
elif command -v xdg-open &>/dev/null; then
  xdg-open "$thing"
elif command -v open &>/dev/null; then
  open "$thing"
else
  echo "error: no suitable open command found" >&2
  exit 1
fi
