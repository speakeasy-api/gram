# Mock Speakeasy IDP

A lightweight mock identity provider for local development and testing. It implements the `/v1/speakeasy_provider/*` endpoints that the Gram server calls during authentication, replacing the need for a real Speakeasy IDP connection.

## How it works

When you click "Login" in the dashboard:

1. The server redirects the browser to the mock IDP's `/login` endpoint
2. The mock IDP auto-approves the login (no credentials needed) and redirects back with an auth code
3. The server exchanges the code for a token, validates it, and creates a session

There is no username/password prompt -- login is instant.

## Package usage

The `mockidp` Go package can be used in two ways:

### In tests

```go
import (
    "net/http/httptest"
    mockidp "github.com/speakeasy-api/gram/mock-speakeasy-idp"
)

cfg := mockidp.NewConfig()  // deterministic defaults, no env vars
srv := httptest.NewServer(mockidp.Handler(cfg))
defer srv.Close()
```

### As a standalone server

```sh
mise run start:mock-idp
```

Or directly:

```sh
go run ./mock-speakeasy-idp/main
```

It runs on `http://localhost:35291` by default.

## Configuration

The mock user and organization are configurable via environment variables. Set these in `mise.local.toml` to customize:

```toml
[env]
MOCK_IDP_USER_EMAIL = "you@example.com"
MOCK_IDP_USER_DISPLAY_NAME = "Your Name"
MOCK_IDP_USER_ADMIN = "true"
MOCK_IDP_ORG_NAME = "My Workspace"
MOCK_IDP_ORG_SLUG = "my-workspace"
```

All variables and their defaults:

| Variable                      | Default                                |
| ----------------------------- | -------------------------------------- |
| `MOCK_IDP_USER_ID`            | `dev-user-1`                           |
| `MOCK_IDP_USER_EMAIL`         | `dev@example.com`                      |
| `MOCK_IDP_USER_DISPLAY_NAME`  | `Dev User`                             |
| `MOCK_IDP_USER_PHOTO_URL`     | _(none)_                               |
| `MOCK_IDP_USER_GITHUB_HANDLE` | _(none)_                               |
| `MOCK_IDP_USER_ADMIN`         | `true`                                 |
| `MOCK_IDP_USER_WHITELISTED`   | `true`                                 |
| `MOCK_IDP_ORG_ID`             | `550e8400-e29b-41d4-a716-446655440000` |
| `MOCK_IDP_ORG_NAME`           | `Local Dev Org`                        |
| `MOCK_IDP_ORG_SLUG`           | `local-dev-org`                        |
| `MOCK_IDP_ORG_ACCOUNT_TYPE`   | `free`                                 |

The secret key used to authenticate server-to-IDP calls is controlled by `SPEAKEASY_SECRET_KEY` (default: `test-secret`), which must match between the server and the mock IDP.

After changing env vars, restart the mock IDP process in [madprocs](https://github.com/speakeasy-api/madprocs) (select it and press `r`).

## Endpoints

| Method | Path                              | Auth         | Description                                   |
| ------ | --------------------------------- | ------------ | --------------------------------------------- |
| `GET`  | `/v1/speakeasy_provider/login`    | None         | Auto-approves login, redirects with auth code |
| `POST` | `/v1/speakeasy_provider/exchange` | Provider key | Exchanges auth code for ID token              |
| `GET`  | `/v1/speakeasy_provider/validate` | Provider key | Validates token, returns user + orgs          |
| `POST` | `/v1/speakeasy_provider/revoke`   | Provider key | Revokes a token                               |
| `POST` | `/v1/speakeasy_provider/register` | Provider key | Creates a new organization                    |
