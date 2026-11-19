#!/usr/bin/env bash

#MISE dir="{{ config_root }}/pystreams"
#MISE description="Start up python stream subscribers"
#MISE hide=true

# With the stub on, `gram streams` already consumes these subscriptions, and a
# second consumer would take a share of their messages.
case "${GRAM_PYSTREAMS_STUB:-}" in
  1 | t | T | true | TRUE | True)
    echo "GRAM_PYSTREAMS_STUB is on, so gram streams stubs pystreams. Set GRAM_PYSTREAMS_STUB=false in mise.local.toml and restart streams to run it." >&2
    exit 1
    ;;
esac

exec uv run multi "$@"
