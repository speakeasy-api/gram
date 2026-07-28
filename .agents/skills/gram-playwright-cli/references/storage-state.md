# Storage Management

Manage cookies, localStorage, sessionStorage, and browser storage state.

## Storage State

Save and restore complete browser state including cookies and storage.

### Save Storage State

```bash
# Save to auto-generated filename (storage-state-{timestamp}.json)
mise run playwright state-save

# Save to specific filename
mise run playwright state-save my-auth-state.json
```

### Restore Storage State

```bash
# Load storage state from file
mise run playwright state-load my-auth-state.json

# Reload page to apply cookies
mise run playwright open https://example.com
```

### Storage State File Format

The saved file contains:

```json
{
  "cookies": [
    {
      "name": "session_id",
      "value": "abc123",
      "domain": "example.com",
      "path": "/",
      "expires": 1893456000,
      "httpOnly": true,
      "secure": true,
      "sameSite": "Lax"
    }
  ],
  "origins": [
    {
      "origin": "https://example.com",
      "localStorage": [
        { "name": "theme", "value": "dark" },
        { "name": "user_id", "value": "12345" }
      ]
    }
  ]
}
```

## Cookies

### List All Cookies

```bash
mise run playwright cookie-list
```

### Filter Cookies by Domain

```bash
mise run playwright cookie-list --domain=example.com
```

### Filter Cookies by Path

```bash
mise run playwright cookie-list --path=/api
```

### Get Specific Cookie

```bash
mise run playwright cookie-get session_id
```

### Set a Cookie

```bash
# Basic cookie
mise run playwright cookie-set session abc123

# Cookie with options
mise run playwright cookie-set session abc123 --domain=example.com --path=/ --httpOnly --secure --sameSite=Lax

# Cookie with expiration (Unix timestamp)
mise run playwright cookie-set remember_me token123 --expires=1893456000
```

### Delete a Cookie

```bash
mise run playwright cookie-delete session_id
```

### Clear All Cookies

```bash
mise run playwright cookie-clear
```

### Advanced: Multiple Cookies or Custom Options

For complex scenarios like adding multiple cookies at once, use `run-code`:

```bash
mise run playwright run-code "async page => {
  await page.context().addCookies([
    { name: 'session_id', value: 'sess_abc123', domain: 'example.com', path: '/', httpOnly: true },
    { name: 'preferences', value: JSON.stringify({ theme: 'dark' }), domain: 'example.com', path: '/' }
  ]);
}"
```

## Local Storage

### List All localStorage Items

```bash
mise run playwright localstorage-list
```

### Get Single Value

```bash
mise run playwright localstorage-get token
```

### Set Value

```bash
mise run playwright localstorage-set theme dark
```

### Set JSON Value

```bash
mise run playwright localstorage-set user_settings '{"theme":"dark","language":"en"}'
```

### Delete Single Item

```bash
mise run playwright localstorage-delete token
```

### Clear All localStorage

```bash
mise run playwright localstorage-clear
```

### Advanced: Multiple Operations

For complex scenarios like setting multiple values at once, use `run-code`:

```bash
mise run playwright run-code "async page => {
  await page.evaluate(() => {
    localStorage.setItem('token', 'jwt_abc123');
    localStorage.setItem('user_id', '12345');
    localStorage.setItem('expires_at', Date.now() + 3600000);
  });
}"
```

## Session Storage

### List All sessionStorage Items

```bash
mise run playwright sessionstorage-list
```

### Get Single Value

```bash
mise run playwright sessionstorage-get form_data
```

### Set Value

```bash
mise run playwright sessionstorage-set step 3
```

### Delete Single Item

```bash
mise run playwright sessionstorage-delete step
```

### Clear sessionStorage

```bash
mise run playwright sessionstorage-clear
```

## IndexedDB

### List Databases

```bash
mise run playwright run-code "async page => {
  return await page.evaluate(async () => {
    const databases = await indexedDB.databases();
    return databases;
  });
}"
```

### Delete Database

```bash
mise run playwright run-code "async page => {
  await page.evaluate(() => {
    indexedDB.deleteDatabase('myDatabase');
  });
}"
```

## Common Patterns

### Authentication State Reuse

```bash
# Step 1: Login and save state
mise run playwright open https://app.example.com/login
mise run playwright snapshot
mise run playwright fill e1 "user@example.com"
mise run playwright fill e2 "password123"
mise run playwright click e3

# Save the authenticated state
mise run playwright state-save auth.json

# Step 2: Later, restore state and skip login
mise run playwright state-load auth.json
mise run playwright open https://app.example.com/dashboard
# Already logged in!
```

### Save and Restore Roundtrip

```bash
# Set up authentication state
mise run playwright open https://example.com
mise run playwright eval "() => { document.cookie = 'session=abc123'; localStorage.setItem('user', 'john'); }"

# Save state to file
mise run playwright state-save my-session.json

# ... later, in a new session ...

# Restore state
mise run playwright state-load my-session.json
mise run playwright open https://example.com
# Cookies and localStorage are restored!
```

## Security Notes

- Never commit storage state files containing auth tokens
- Add `*.auth-state.json` to `.gitignore`
- Delete state files after automation completes
- Use environment variables for sensitive data
- By default, sessions run in-memory mode which is safer for sensitive operations
