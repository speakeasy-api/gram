package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
)

// The token-command credential source authenticates a headless workload box
// (ADR-0025). The command comes from SPEAKEASY_AI_HOOKS_TOKEN_COMMAND, identity
// discovery, or the token-box record, and is then the only source: any key
// would bypass the server's per-project check. The project is the box's
// configured hooks project (the device agent's
// workload_identity.hooks_project_slug), delivered in the command's output;
// the agent's org:hooks_ingest RBAC decides whether the server accepts it.

const (
	tokenCommandTimeout   = 5 * time.Second
	tokenCommandMaxStdout = 16 << 10
	tokenCommandMaxStderr = 2 << 10
	tokenRefreshMargin    = 60 * time.Second
	tokenMaxBearerBytes   = 8 << 10
	tokenCacheKind        = "token"

	// tokenCommandRefusedExit is the command's exit status for a definitive
	// refusal (revoked, not a workload, no configured hooks project), as
	// opposed to an outage.
	tokenCommandRefusedExit = 3
)

// errTokenRefused marks a token command failure as a definitive refusal.
var errTokenRefused = errors.New("the device agent refused to issue a hooks token")

// errNoHooksProject is the refusal for command output without a usable
// project. It is definitive like exit status 3: there is no other source of
// the project, and a key would bypass the server's per-project check.
var errNoHooksProject = fmt.Errorf("%w: the device agent has no valid workload_identity.hooks_project_slug configured; hooks are not sent", errTokenRefused)

// hooksProjectSlugPattern matches the device agent's validation of
// workload_identity.hooks_project_slug.
var hooksProjectSlugPattern = regexp.MustCompile(`^[a-z0-9_-]{1,128}$`)

func validHooksProjectSlug(slug string) bool { return hooksProjectSlugPattern.MatchString(slug) }

// A refused token box fails closed until a fresh token is accepted; otherwise an
// expired token would reach the fail-open no-token path. A separate file, so a
// damaged token cache cannot clear it.
func tokenRejectedPath() string { return authFilePath() + ".token-rejected" }

// tokenBoxPath records that this machine authenticates by token (argv, project,
// refused). It lives apart from the auth cache so losing a bearer never erases it.
func tokenBoxPath() string { return authFilePath() + ".token-box" }

type tokenBoxState struct {
	Argv     []string `json:"argv"`
	Project  string   `json:"project,omitempty"`
	Rejected bool     `json:"rejected,omitempty"`
}

// readTokenBox returns the record and whether this is a token box. A record
// that exists but cannot be read still marks a token box: uncertainty must
// never reopen key fallback.
func readTokenBox() (tokenBoxState, bool) {
	b, err := os.ReadFile(tokenBoxPath())
	if errors.Is(err, os.ErrNotExist) {
		return tokenBoxState{}, false
	}
	var st tokenBoxState
	if err != nil || json.Unmarshal(b, &st) != nil {
		return tokenBoxState{Argv: nil, Project: "", Rejected: true}, true
	}
	return st, true
}

func writeTokenBox(st tokenBoxState) error {
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("encode token box: %w", err)
	}
	return atomicWriteCacheFile(tokenBoxPath(), append(b, '\n'))
}

// markTokenRejected records a definitive refusal twice, in the marker file and
// in the token-box record, so a single failed write cannot let the box fail
// open later. It reports an error only when neither record was written.
func markTokenRejected() error {
	f, markerErr := os.OpenFile(tokenRejectedPath(), os.O_CREATE|os.O_WRONLY, 0o600)
	if markerErr == nil {
		markerErr = f.Close()
	}
	var boxErr error
	updateTokenBox(func(st *tokenBoxState) { st.Rejected = true }, &boxErr)
	if markerErr != nil && boxErr != nil {
		return fmt.Errorf("record token refusal: %w; %w", markerErr, boxErr)
	}
	return nil
}

// updateTokenBox applies fn under a cross-process lock, so a live send and a
// drain cannot lose each other's Rejected flag. The write error goes to errOut.
func updateTokenBox(fn func(*tokenBoxState), errOut *error) {
	withFileLock(tokenBoxPath(), func() {
		st, _ := readTokenBox()
		fn(&st)
		*errOut = writeTokenBox(st)
	})
}

// tokenRejected reports whether this box must fail closed. Anything other than
// a clean "no marker, record says not rejected" counts as rejected.
func tokenRejected() bool {
	if tokenPersistFailed.Load() {
		return true
	}
	if _, err := os.Stat(tokenRejectedPath()); !errors.Is(err, os.ErrNotExist) {
		return true
	}
	st, ok := readTokenBox()
	return ok && st.Rejected
}

func clearTokenRejected() {
	if _, ok := readTokenBox(); ok {
		var err error
		updateTokenBox(func(st *tokenBoxState) { st.Rejected = false }, &err)
		if err != nil {
			// Keep the marker: the record still says rejected.
			return
		}
	}
	_ = os.Remove(tokenRejectedPath())
}

// tokenPersistFailed is set when writeTokenAuth fails. The box is then treated
// as refused until a complete token write succeeds.
var tokenPersistFailed atomic.Bool

// nowFunc is the relay's clock for token expiry; a var so tests can move it.
var nowFunc = time.Now

// runTokenCommand executes the token command's argv; a var so tests can
// script its output without a real binary.
var runTokenCommand = execTokenCommand

// tokenCommandArgv returns the argv to authenticate with and whether the token
// source applies. An applying source with a bad argv still never falls back to a key.
func tokenCommandArgv() ([]string, bool) {
	if commandLine := Env("HOOKS_TOKEN_COMMAND"); commandLine != "" {
		return strings.Fields(commandLine), true
	}
	if id := deviceIdentityFor(context.Background()); id.Workload {
		return id.HooksTokenArgv, true
	}
	// Once a box has authenticated by token it stays a token box, whatever a
	// (possibly stale or failed) identity lookup now says: falling back to a
	// key would bypass the server's per-project check.
	if st, ok := readTokenBox(); ok {
		return st.Argv, true
	}
	return nil, false
}

// tokenCommandConfigured reports whether this machine authenticates through
// the token command.
func tokenCommandConfigured() bool {
	_, ok := tokenCommandArgv()
	return ok
}

type tokenCommandOutput struct {
	Authorization string    `json:"authorization"`
	Project       string    `json:"project"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// resolveTokenAuth returns the cached token while it has tokenRefreshMargin
// left, else a fresh one. refresh skips the cache, for 401/403 recovery.
func resolveTokenAuth(cfg Config, argv []string, refresh bool) (creds, bool) {
	if !refresh {
		if c, ok := readCachedToken(cfg); ok {
			return c, true
		}
	}
	out, err := runTokenCommand(context.Background(), argv, refresh)
	if err != nil {
		if errors.Is(err, errTokenRefused) {
			markTokenRejected()
		}
		debugTokenCommand(cfg, "token command failed: %v", err)
		return creds{}, false
	}
	bearer, err := parseTokenOutput(out)
	if err != nil {
		if errors.Is(err, errTokenRefused) {
			markTokenRejected()
		}
		debugTokenCommand(cfg, "token command output rejected: %v", err)
		return creds{}, false
	}
	c := creds{
		ServerURL: cfg.ServerURL,
		APIKey:    "",
		Project:   bearer.Project,
		Email:     "",
		Org:       cfg.OrgID,
		Source:    credToken,
		Bearer:    strings.TrimPrefix(bearer.Authorization, "Bearer "),
		ExpiresAt: bearer.ExpiresAt,
	}
	if err := writeTokenAuth(c, argv); err != nil {
		// Persisting the token-box record is a precondition for using the
		// token: without it a later failed identity lookup could fall back to
		// a key. Fail closed instead.
		tokenPersistFailed.Store(true)
		debugTokenCommand(cfg, "token cache write failed, token not used: %v", err)
		return creds{}, false
	}
	tokenPersistFailed.Store(false)
	return c, true
}

// parseTokenOutput validates the command's JSON. A value with a line break or
// stray whitespace is refused: the cache is line-oriented and the bearer goes
// into a header verbatim. A missing or malformed project is a definitive
// refusal (errNoHooksProject), not an outage.
func parseTokenOutput(raw []byte) (tokenCommandOutput, error) {
	var out tokenCommandOutput
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return tokenCommandOutput{}, fmt.Errorf("decode: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return tokenCommandOutput{}, errors.New("trailing data after the JSON object")
	}
	token, ok := strings.CutPrefix(out.Authorization, "Bearer ")
	switch {
	case !ok || token == "":
		return tokenCommandOutput{}, errors.New("authorization is not a bearer token")
	case len(token) > tokenMaxBearerBytes || strings.ContainsAny(token, " \t\r\n"):
		return tokenCommandOutput{}, errors.New("bearer token is malformed")
	case out.ExpiresAt.IsZero() || !out.ExpiresAt.After(nowFunc()):
		return tokenCommandOutput{}, errors.New("expires_at is missing or in the past")
	case !validHooksProjectSlug(out.Project):
		return tokenCommandOutput{}, errNoHooksProject
	}
	return out, nil
}

// readCachedToken loads the cached token if it belongs to this deployment and
// has tokenRefreshMargin left. Its project is the box's configured hooks
// project, not the plugin's; a cache without a valid one is not used.
func readCachedToken(cfg Config) (creds, bool) {
	values, err := readAuthFile(authFilePath())
	if err != nil || values["kind"] != tokenCacheKind {
		return creds{}, false
	}
	expires, err := time.Parse(time.RFC3339Nano, values["expires_at"])
	if err != nil || values["bearer"] == "" || !validHooksProjectSlug(values["project"]) ||
		!sameDeployment(values["server_url"], values["org"], cfg.ServerURL, cfg.OrgID) ||
		!nowFunc().Add(tokenRefreshMargin).Before(expires) {
		return creds{}, false
	}
	return creds{
		ServerURL: values["server_url"],
		APIKey:    "",
		Project:   values["project"],
		Email:     "",
		Org:       values["org"],
		Source:    credToken,
		Bearer:    values["bearer"],
		ExpiresAt: expires,
	}, true
}

// writeTokenAuth caches a token credential and marks the machine established,
// so later failures follow the fail-closed ratchet like any credential. The
// token-box record is written first: it is what keeps the box off keys.
func writeTokenAuth(c creds, argv []string) error {
	for _, v := range []string{c.ServerURL, c.Bearer, c.Project, c.Org} {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("credential value contains a line break")
		}
	}
	var boxErr error
	updateTokenBox(func(st *tokenBoxState) {
		// Never clear a refusal here: only an accepted token does that.
		st.Argv, st.Project = argv, c.Project
	}, &boxErr)
	if boxErr != nil {
		return boxErr
	}
	body := fmt.Sprintf("kind=%s\nserver_url=%s\nbearer=%s\nproject=%s\norg=%s\nexpires_at=%s\n",
		tokenCacheKind, c.ServerURL, c.Bearer, c.Project, c.Org, c.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if err := atomicWriteCacheFile(authFilePath(), []byte(body)); err != nil {
		return err
	}
	markEstablished()
	return nil
}

// recoverTokenRejection is shared by the live send and the drain: one fresh token
// and one resend if the project matches; a failed refresh or rejected resend
// records the refusal and forgets the bearer.
func recoverTokenRejection(cfg Config, c creds, res ingestResult, resend func(creds) ingestResult) (ingestResult, creds) {
	argv, _ := tokenCommandArgv()
	fresh, ok := resolveTokenAuth(cfg, argv, true)
	if ok && fresh.Project != c.Project {
		// The box's configured hooks project changed: this event was bound to
		// the old one, so it is not resent. The fresh token stays cached.
		debugTokenCommand(cfg, "token project changed from %q to %q; event not resent", c.Project, fresh.Project)
		return res, c
	}
	if ok {
		res, c = resend(fresh), fresh
	}
	if res.authRejected || !ok {
		if res.authRejected {
			// A 401/403 may be policy rather than a bad token: the agent's
			// org:hooks_ingest RBAC may not cover this project.
			debugTokenCommand(cfg, "hooks refused for project %q (status %d): the workload's agent may not send hooks there, or the token was rejected", c.Project, res.statusCode)
		}
		if err := markTokenRejected(); err != nil {
			debugTokenCommand(cfg, "%v", err)
		}
		forgetAuth()
	}
	return res, c
}

// tokenCommandEnvAllow is the only environment passed to the command: enough to
// find the user's state and the agent's config, and no credentials.
var tokenCommandEnvAllow = map[string]bool{
	"HOME": true, "PATH": true, "USER": true, "LOGNAME": true, "TMPDIR": true,
	"XDG_CONFIG_HOME": true, "XDG_STATE_HOME": true, "XDG_RUNTIME_DIR": true,
	"SystemRoot": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "ProgramData": true,
	"SPEAKEASY_MANAGED_CONFIG": true, "SPEAKEASY_MANAGED_PREFS": true, "SPEAKEASY_LOCAL_CONFIG": true,
	"SPEAKEASY_SOCKET": true, "SPEAKEASY_CONTROL_PLANE_URL": true, "SPEAKEASY_AUTH_URL": true,
	"SPEAKEASY_KEYCHAIN_SERVICE": true,
}

func tokenCommandEnv(refresh bool) []string {
	var env []string
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); tokenCommandEnvAllow[name] {
			env = append(env, kv)
		}
	}
	if refresh {
		env = append(env, "SPEAKEASY_AI_HOOKS_TOKEN_REFRESH=1")
	}
	return env
}

// execTokenCommand runs argv without a shell. The first element must be an
// absolute path, so PATH lookups cannot substitute a different binary.
func execTokenCommand(ctx context.Context, argv []string, refresh bool) ([]byte, error) {
	if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return nil, errors.New("the hooks token command (SPEAKEASY_AI_HOOKS_TOKEN_COMMAND or the device agent's hooks_token_argv) must start with an absolute path")
	}
	ctx, cancel := context.WithTimeout(ctx, tokenCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- the administrator's or device agent's command, run without a shell.
	cmd.Env = tokenCommandEnv(refresh)
	cmd.Stdin = nil
	var stdout, stderr cappedBuffer
	stdout.limit, stderr.limit = tokenCommandMaxStdout, tokenCommandMaxStderr
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == tokenCommandRefusedExit {
			err = fmt.Errorf("%w: %w", errTokenRefused, err)
		}
		msg := sanitizeCommandStderr(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	if stdout.overflow {
		return nil, fmt.Errorf("output exceeds %d bytes", tokenCommandMaxStdout)
	}
	return stdout.Bytes(), nil
}

// cappedBuffer keeps at most limit bytes and records whether more arrived.
// The buffer is a named field, not embedded: an embedded bytes.Buffer would
// promote ReadFrom, and io.Copy would use it to bypass the cap in Write.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room < len(p) {
		b.overflow = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *cappedBuffer) String() string { return b.buf.String() }

var _ io.Writer = (*cappedBuffer)(nil)

// sanitizeCommandStderr keeps the first line of the command's stderr for the
// debug log, masking every word that is not plain letters: a failing command
// must not leak a credential it was handling, however short.
func sanitizeCommandStderr(raw string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(raw), "\n")
	fields := strings.Fields(line)
	for i, f := range fields {
		if !plainWord(strings.TrimRight(f, ",.:;!?")) {
			fields[i] = "***"
		}
	}
	out := strings.Join(fields, " ")
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func plainWord(w string) bool {
	if w == "" || len(w) > 32 {
		return false
	}
	for _, r := range w {
		if !unicode.IsLetter(r) && r != '\'' {
			return false
		}
	}
	return true
}

func debugTokenCommand(cfg Config, format string, args ...any) {
	debugLogf(cfg.DebugLog, format, args...)
}
