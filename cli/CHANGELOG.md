# @gram/cli

## 0.9.0

### Minor Changes

- 6ac98df: Add whoami command to easily view details about the current profile specified in $HOME/.gram/profile.json
- 1470223: Support automated authentication for any user profile via `gram auth`

## 0.8.0

### Minor Changes

- fde5a08: Support function uploads
- c173592: Add profile support to CLI for storing and managing credentials. Users can now save their authentication credentials in named profiles, eliminating the need to pass them as explicit environment variables for each command invocation.

## 0.4.0

### Minor Changes

- d6923b6: Enable asset upload to gram via `gram upload`

### Patch Changes

- 38e7b8f: Release CLI with properly prefixed tags.
- 40f0565: Increase client timeout to 10 minutes

## 0.3.0

### Minor Changes

- fa60d03: Support YAML and TOML deployment configs
- e29c090: Implement status command

### Patch Changes

- 9d23ef1: Initial changelog entry for Gram CLI
