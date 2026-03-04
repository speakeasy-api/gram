#!/usr/bin/env bash

#MISE description="Test the Gram hooks Claude plugin locally (from marketplace)"
#MISE dir="{{ config_root }}"

claude plugin marketplace add ./hooks
claude plugin install gram-hooks@gram