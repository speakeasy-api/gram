#!/usr/bin/env bash

set -e

pnpm install
pnpm lint
pnpm build
