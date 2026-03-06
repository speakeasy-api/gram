#!/usr/bin/env bash
#MISE dir="{{ config_root }}/server"
#MISE description="Start up the Temporal worker"

# Use air for hot reload - args after -- are passed to the binary
air -- worker
