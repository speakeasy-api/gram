#!/usr/bin/env bash
# Local fixture wrapper: generated Cursor plugins receive their own login.sh.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$script_dir/../../plugin-claude/hooks/login.sh" "$@"
