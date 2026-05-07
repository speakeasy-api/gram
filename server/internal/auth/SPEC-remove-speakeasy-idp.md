# Remove Speakeasy IDP from Gram Auth

## Context

Gram currently delegates authentication to a "Speakeasy IDP" — making HTTP calls to exchange codes, validate tokens, create orgs, and revoke sessions. The session ID stored in Redis IS the IDP's opaque token. This couples Gram to the Speakeasy registry for every login, every cache miss, and every org operation.

**Goal**: Remove this coupling. Gram should exchange codes for JWTs via standard OIDC, mint its own session IDs (UUIDs), and look up org memberships from its own DB. A WorkOS sync job (Tiago) will keep org/membership data fresh; the callback uses WorkOS API as fallback for new users.

## Decisions

| Question                     | Answer                                                 |
| ---------------------------- | ------------------------------------------------------ |
| Org membership on cache miss | **Hybrid**: DB first, WorkOS API fallback if zero orgs |
| Register endpoint            | **Keep**, create org locally in DB                     |
| syncWorkOSMemberships        | **Remove** — sync job handles it                       |
| Env var naming               | **Rename** SPEAKEASY\_\* → new names                   |

## Skills to Activate

`golang`, `postgresql`, `gram-management-api`

---

## Step 1 — New SQL query: ListOrganizationsForUser

**File**: `server/internal/organizations/queries.sql`

Add query to fetch a user's org memberships from DB:

```sql
-- name: ListOrganizationsForUser :many
SELECT om.id, om.name, om.slug, om.workos_id
FROM organization_user_relationships our
JOIN organization_metadata om ON om.id = our.organization_id
WHERE our.user_id = @user_id
  AND our.deleted_at IS NULL
  AND om.disabled_at IS NULL;
```

Run `mise db:gen` to regenerate Go code.

---

## Step 2 — Delete speakeasyconnections.go

**Delete**:

- `server/internal/auth/sessions/speakeasyconnections.go`
- `server/internal/auth/sessions/speakeasyconnections_test.go`

This removes: `ExchangeTokenFromSpeakeasy`, `GetUserInfoFromSpeakeasy`, `CreateOrgFromSpeakeasy`, `RevokeTokenFromSpeakeasy`, `BuildAuthorizationURL`, `HasAccessToOrganization`, `GetUserInfo`, `InvalidateUserInfoCache`, `syncWorkOSMemberships`.

Functions that must be reimplemented (Steps 3-4): `GetUserInfo`, `HasAccessToOrganization`, `InvalidateUserInfoCache`, `BuildAuthorizationURL`.

---

## Step 3 — Update Manager struct + constructor

**File**: `server/internal/auth/sessions/sessions.go`

Remove from struct:

- `speakeasyServerAddress string`
- `speakeasySecretKey string`
- `speakeasyClient *guardian.HTTPClient`

Add to struct:

- `idpBaseURL string` — OIDC base URL (e.g. `http://localhost:35291/oauth2`)
- `idpHTTPClient *guardian.HTTPClient`

Update `NewManager` signature — drop `speakeasyServerAddress`, `speakeasySecretKey` params, add `idpBaseURL`. Drop `workos *workos.Client` param (no longer needed in session manager — sync job handles WorkOS).

---

## Step 4 — New file: oidc.go (replaces speakeasyconnections.go)

**File**: `server/internal/auth/sessions/oidc.go` (new)

### 4a. ExchangeCodeForTokens

```go
func (s *Manager) ExchangeCodeForTokens(ctx context.Context, code, redirectURI string) (accessToken string, err error)
```

- POST `{idpBaseURL}/token` with `grant_type=authorization_code`, `code`, `redirect_uri`
- Parse JSON response, return `access_token`

### 4b. FetchUserInfoFromIDP

```go
func (s *Manager) FetchUserInfoFromIDP(ctx context.Context, accessToken string) (*IDPUserInfo, error)
```

- GET `{idpBaseURL}/userinfo` with `Authorization: Bearer {accessToken}`
- Returns `sub`, `email`, `name`, `picture`

### 4c. BuildUserInfoFromDB (replaces GetUserInfoFromSpeakeasy)

```go
func (s *Manager) BuildUserInfoFromDB(ctx context.Context, userID string) (*CachedUserInfo, error)
```

- `userRepo.GetUser(ctx, userID)` for user data
- `orgRepo.ListOrganizationsForUser(ctx, userID)` for org memberships
- If zero orgs AND workos client available → call WorkOS to bootstrap memberships, then retry
- Build and return `CachedUserInfo`

### 4d. GetUserInfo (reimplemented)

```go
func (s *Manager) GetUserInfo(ctx context.Context, userID string) (*CachedUserInfo, bool, error)
```

- Cache hit → return
- Cache miss → `BuildUserInfoFromDB` → store in cache → return
- Note: drops `sessionID` param (no longer needed — was only used to call Speakeasy /validate)

### 4e. HasAccessToOrganization (reimplemented)

Same logic as before, calls `GetUserInfo` then checks org list.

### 4f. InvalidateUserInfoCache (reimplemented)

Same logic as before.

### 4g. BuildAuthorizationURL (reimplemented)

```go
func (s *Manager) BuildAuthorizationURL(ctx context.Context, params AuthURLParams) (*url.URL, error)
```

- Build: `{idpBaseURL}/authorize?response_type=code&redirect_uri={callbackURL}&state={state}&scope=openid+email+profile`

---

## Step 5 — Update types.go

**File**: `server/internal/auth/sessions/types.go`

- Change `UserInfoCacheKey` prefix: `"speakeasyUserInfo:"` → `"userInfo:"`
- Update `Organization` comment (no longer "from Speakeasy IDP response")

---

## Step 6 — Update auth handlers (impl.go)

**File**: `server/internal/auth/impl.go`

### AuthConfigurations

```go
type AuthConfigurations struct {
    IDPBaseURL       string  // was SpeakeasyServerAddress
    GramServerURL    string
    SignInRedirectURL string
    Environment      string
}
```

### Callback

Replace:

```go
idToken, err := s.sessions.ExchangeTokenFromSpeakeasy(ctx, payload.Code)
userInfo, err := s.sessions.GetUserInfoFromSpeakeasy(ctx, idToken)
session := sessions.Session{SessionID: idToken, ...}
```

With:

```go
callbackURL := s.callbackURL(ctx)  // same URL used in Login redirect
accessToken, err := s.sessions.ExchangeCodeForTokens(ctx, payload.Code, callbackURL)
idpUser, err := s.sessions.FetchUserInfoFromIDP(ctx, accessToken)
user, err := s.upsertUserFromIDP(ctx, idpUser)  // upsert in users table
userInfo, err := s.sessions.BuildUserInfoFromDB(ctx, user.ID)
sessionID := uuid.New().String()  // mint our own session ID
session := sessions.Session{SessionID: sessionID, UserID: user.ID, ...}
```

### Login

Replace `s.cfg.SpeakeasyServerAddress + "/v1/speakeasy_provider/login?..."` with `s.sessions.BuildAuthorizationURL(...)`.

### Logout

Remove `s.sessions.RevokeTokenFromSpeakeasy(...)`. Just clear session + invalidate cache.

### Register

Replace `s.sessions.CreateOrgFromSpeakeasy(...)` with local DB operations:

- Create org in `organization_metadata`
- Create user-org relationship in `organization_user_relationships`
- Invalidate user info cache
- Update session with new active org

### Info

Update `s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)` → `s.sessions.GetUserInfo(ctx, authCtx.UserID)` (drop sessionID param).

### SwitchScopes

Same GetUserInfo signature change.

---

## Step 7 — Update Gram OAuth provider

**File**: `server/internal/oauth/providers/gram.go`

Replace `ExchangeTokenFromSpeakeasy` + `GetUserInfoFromSpeakeasy` with:

```go
accessToken, err := p.sessions.ExchangeCodeForTokens(ctx, code, callbackURL)
idpUser, err := p.sessions.FetchUserInfoFromIDP(ctx, accessToken)
// upsert user, build user info from DB, check org access, mint session
```

**File**: `server/internal/oauth/impl.go` line 379
`s.sessions.BuildAuthorizationURL(...)` — same method, new implementation. No change needed here.

---

## Step 8 — Update CLI flags + wiring

**File**: `server/cmd/gram/start.go`

- Replace flags: `speakeasy-server-address` / `speakeasy-secret-key` → `idp-base-url` (env: `GRAM_IDP_BASE_URL`)
- Update `sessions.NewManager(...)` call (new signature)
- Update `auth.AuthConfigurations{}` (IDPBaseURL instead of SpeakeasyServerAddress)

**File**: `server/cmd/gram/worker.go`

- Same flag + NewManager changes

**File**: `mise.toml`

- Replace `SPEAKEASY_SERVER_ADDRESS` / `SPEAKEASY_SECRET_KEY` with `GRAM_IDP_BASE_URL = "{{env.GRAM_DEVIDP_EXTERNAL_URL}}/oauth2"`

---

## Step 9 — Update Authenticate (sessions.go)

**File**: `server/internal/auth/sessions/sessions.go`

`Authenticate` method: Update `HasAccessToOrganization` call — drop `sessionID` arg if signature changed.

---

## Step 10 — Update tests

**File**: `server/internal/auth/setup_test.go`

- Mock server: replace `/v1/speakeasy_provider/*` handlers with `/oauth2/token`, `/oauth2/userinfo` handlers
- Update `sessions.NewManager(...)` calls
- Update `auth.AuthConfigurations{}`

**File**: `server/internal/testenv/auth.go`

- `NewTestManager`: update constructor call, point at mock OIDC endpoints
- `InitAuthContext`: replace `ExchangeTokenFromSpeakeasy` + `GetUserInfoFromSpeakeasy` with new flow (ExchangeCodeForTokens + FetchUserInfoFromIDP + BuildUserInfoFromDB), mint UUID session

**File**: `dev-idp/pkg/testidp/testidp.go`

- Add `/oauth2/token` and `/oauth2/userinfo` handlers to test mock (or update existing mock to serve both protocols during migration)

**File**: `server/internal/auth/sessions/speakeasyconnections_test.go` — already deleted in Step 2. WorkOS sync tests need to move or be dropped (sync job will have its own tests).

---

## Verification

1. `mise db:gen` — regenerate SQL after adding ListOrganizationsForUser
2. `mise build:server` — compile check
3. `mise lint:server` — lint pass
4. Run auth tests: `cd server && go test ./internal/auth/... -v`
5. Run sessions tests: `cd server && go test ./internal/auth/sessions/... -v`
6. Run testenv-dependent tests: `cd server && go test ./internal/... -run TestCallback -v`
7. Manual: `madprocs` → login flow → verify session works, org switching works, logout works
