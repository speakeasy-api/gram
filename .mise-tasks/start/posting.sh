#!/usr/bin/env bash
#MISE dir="{{ config_root }}"
#MISE description="Open Posting collection to play with the API"

set -e

if [ ! -d posting ] || [ -z "$(ls -A posting)" ]; then
    mise run gen:posting-server
fi

uvx posting --collection posting