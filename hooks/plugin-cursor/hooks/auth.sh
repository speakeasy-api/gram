#!/usr/bin/env bash
# Local fixture wrapper: generated Cursor plugins receive their own auth.sh.

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
. "$script_dir/../../plugin-claude/hooks/auth.sh"
