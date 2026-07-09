#!/usr/bin/env bash

#MISE description="Start the Gram Dashboard Storybook (design system workbench)"

set -e

exec pnpm --filter ./client/dashboard storybook
