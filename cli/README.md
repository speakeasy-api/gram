# Speakeasy AI Control Plane CLI

The `speakeasy` command line interface for the Speakeasy AI Control Plane.

## Install

```bash
# Homebrew (macOS and Linux)
brew install speakeasy-api/tap/cli

# npm
npm i -g @speakeasy-api/cli
```

Then authenticate and check your setup:

```bash
speakeasy auth
speakeasy whoami
```

The Homebrew `cli` formula conflicts with the Speakeasy SDK generator formula
(`speakeasy-api/tap/speakeasy`), because both install a binary named
`speakeasy`. Install one or the other with Homebrew.

### Migrating from `gram`

This CLI was previously named `gram`. The `speakeasy-api/tap/gram` Homebrew
formula still installs it under the `gram` name for now, and running it as
`gram` prints a deprecation notice. Switch to the `speakeasy` command.
Profiles stay in `~/.gram/profile.json`, so you do not need to authenticate
again.

## Local Development

1. Setup environment
   - `export GRAM_API_URL=https://localhost:8080`
   - `export GRAM_DASHBOARD_URL=https://localhost:5173`
   - `export GRAM_ORG=organization-123`
   - `export GRAM_PROJECT=default`
   - `export GRAM_API_KEY=<API-KEY>`

2. Run desired command
   - `cd cli`
   - `go run main.go status`

### Testing Gram Functions

1. Stage zip
   - `go run main.go stage function --slug test-fn --location fixtures/example.zip`
   - _You never need to do this again as long as you are using the same zip_

2. Push
   - `go run main.go push`
