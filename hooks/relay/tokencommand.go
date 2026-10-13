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
	"strings"
	"time"
	"unicode"
)

// The token-command credential source (ADR-0025, contracts C7 and C8)
// authenticates a headless workload box. The command is the device agent's
// `speakeasyd hooks-token`, which prints a short-lived device-agent bearer and
// the project the box's admission names. It is found in one of three ways, in
// order:
//
//  1. SPEAKEASY_AI_HOOKS_TOKEN_COMMAND, an explicit override, split on
//     whitespace (no quoting).
//  2. Discovery: `speakeasyd identity` reports "workload": true and the argv in
//     "hooks_token_argv", executed as given.
//  3. The token-box record (tokenBoxPath): once a box has authenticated by
//     token it never drops back to a key because an identity lookup failed or
//     answered from a stale cache. Removing that file is the only way back to
//     key authentication.
//
// Whichever applies, it is the only credential source: an env key, a cached
// personal key or the baked org-wide key would all bypass the admission's
// project filter. The bearer is cached in the auth cache until shortly before
// it expires, so a hook event costs a file read rather than a process spawn.

const (
	tokenCommandTimeout   = 5 * time.Second
	tokenCommandMaxStdout = 16 << 10
	tokenCommandMaxStderr = 2 << 10
	tokenRefreshMargin    = 60 * time.Second
	tokenMaxBearerBytes   = 8 << 10
	tokenCacheKind        = "token"

	// tokenCommandRefusedExit is the exit status the token command uses for a
	// definitive refusal (the device was revoked, is not a workload, or has no
	// hooks project), as opposed to any other failure such as an unreachable
	// control plane.
	tokenCommandRefusedExit = 3
)

// errTokenRefused marks a token command failure as a definitive refusal.
var errTokenRefused = errors.New("the device agent refused to issue a hooks token")

// A token box that has been definitively refused, by the server rejecting
// its token or by the command exiting tokenCommandRefusedExit, fails closed
// until a fresh token is accepted. Without the marker, a refused box would
// reach the no-token path once its cached token expired, and that path follows
// the org's fail-open posture: a revoked device would get through. The marker
// is a separate file, so a damaged token cache cannot clear it.
func tokenRejectedPath() string { return authFilePath() + ".token-rejected" }

// tokenBoxPath records that this machine authenticates by token: the argv that
// mints the token, the project its admission authorizes, and whether it was
// refused. It lives apart from the auth cache, so forgetting a rejected bearer
// or a damaged cache never erases the proof that keys must not be used.
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
	st, _ := readTokenBox()
	st.Rejected = true
	boxErr := writeTokenBox(st)
	if markerErr != nil && boxErr != nil {
		return fmt.Errorf("record token refusal: %w; %w", markerErr, boxErr)
	}
	return nil
}

// tokenRejected reports whether this box must fail closed. Anything other than
// a clean "no marker, record says not rejected" counts as rejected.
func tokenRejected() bool {
	if _, err := os.Stat(tokenRejectedPath()); !errors.Is(err, os.ErrNotExist) {
		return true
	}
	st, ok := readTokenBox()
	return ok && st.Rejected
}

func clearTokenRejected() {
	_ = os.Remove(tokenRejectedPath())
	if st, ok := readTokenBox(); ok && st.Rejected {
		st.Rejected = false
		_ = writeTokenBox(st)
	}
}

// nowFunc is the relay's clock for token expiry; a var so tests can move it.
var nowFunc = time.Now

// runTokenCommand executes the token command's argv; a var so tests can
// script its output without a real binary.
var runTokenCommand = execTokenCommand

// tokenCommandArgv returns the argv this machine must authenticate with, and
// whether the token source applies at all. An applying source with an empty
// or invalid argv is still exclusive: it fails as credential-unavailable and
// never falls back to a key.
func tokenCommandArgv() ([]string, bool) {
	if commandLine := Env("HOOKS_TOKEN_COMMAND"); commandLine != "" {
		return strings.Fields(commandLine), true
	}
	if id := deviceIdentityFor(context.Background()); id.Workload {
		return id.HooksTokenArgv, true
	}
	// Once a box has authenticated by token it stays a token box, whatever a
	// (possibly stale or failed) identity lookup now says: falling back to a
	// key would bypass the admission's project filter.
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

// resolveTokenAuth returns a usable token credential: the cached one while it
// has more than tokenRefreshMargin left, else a fresh one from the command.
// refresh skips the cache and asks the command for a new token, for the
// 401/403 recovery path.
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
		// The token still authenticates this event; the next one re-runs the
		// command.
		debugTokenCommand(cfg, "token cache write failed: %v", err)
	}
	return c, true
}

// parseTokenOutput validates the command's JSON. A value with a line break or
// stray whitespace is refused: the cache is line-oriented and the bearer goes
// into a header verbatim.
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
	case out.Project == "" || strings.ContainsAny(out.Project, " \t\r\n"):
		return tokenCommandOutput{}, errors.New("project is missing or malformed")
	case out.ExpiresAt.IsZero() || !out.ExpiresAt.After(nowFunc()):
		return tokenCommandOutput{}, errors.New("expires_at is missing or in the past")
	}
	return out, nil
}

// readCachedToken loads the cached token when it was minted for this
// deployment and still has more than tokenRefreshMargin left. The project is
// the token's own: it is the project the admission authorizes, not the
// plugin's.
func readCachedToken(cfg Config) (creds, bool) {
	values, err := readAuthFile(authFilePath())
	if err != nil || values["kind"] != tokenCacheKind {
		return creds{}, false
	}
	expires, err := time.Parse(time.RFC3339Nano, values["expires_at"])
	if err != nil || values["bearer"] == "" ||
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
	st, _ := readTokenBox()
	st.Argv, st.Project = argv, c.Project
	if err := writeTokenBox(st); err != nil {
		return err
	}
	body := fmt.Sprintf("kind=%s\nserver_url=%s\nbearer=%s\nproject=%s\norg=%s\nexpires_at=%s\n",
		tokenCacheKind, c.ServerURL, c.Bearer, c.Project, c.Org, c.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if err := atomicWriteCacheFile(authFilePath(), []byte(body)); err != nil {
		return err
	}
	markEstablished()
	return nil
}

// recoverTokenRejection handles a definitive rejection of token credentials c.
// The live send and the spool drain both use it, so their handling cannot
// drift: one fresh token and one resend, never into a different project than
// the rejected event was bound to; if that does not succeed, the refusal is
// recorded and the rejected bearer forgotten (the token-box record survives).
func recoverTokenRejection(cfg Config, c creds, res ingestResult, resend func(creds) ingestResult) (ingestResult, creds) {
	argv, _ := tokenCommandArgv()
	fresh, ok := resolveTokenAuth(cfg, argv, true)
	if ok && fresh.Project != c.Project {
		// The admission now names another project: this event was bound to
		// the old one, so it is not resent. The fresh token stays cached.
		debugTokenCommand(cfg, "token project changed from %q to %q; event not resent", c.Project, fresh.Project)
		return res, c
	}
	if ok {
		res, c = resend(fresh), fresh
	}
	if res.authRejected || !ok {
		if err := markTokenRejected(); err != nil {
			debugTokenCommand(cfg, "%v", err)
		}
		forgetAuth()
	}
	return res, c
}

// tokenCommandEnvAllow lists the environment passed to the command: enough to
// locate the user's state and the device agent's own configuration, and
// nothing that carries a credential. It is an explicit list: any other name,
// SPEAKEASY_* included, is withheld.
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
