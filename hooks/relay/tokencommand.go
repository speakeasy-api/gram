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
//  3. When the device agent cannot be asked at all, the argv recorded with the
//     last cached token, so a box already authenticating by token never drops
//     back to a key because one identity lookup failed.
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
)

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
	id := deviceIdentityFor(context.Background())
	if id.Workload {
		return id.HooksTokenArgv, true
	}
	if !id.Known {
		if argv, ok := cachedTokenArgv(); ok {
			return argv, true
		}
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
	if dec.More() {
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
// argv that minted it is recorded too (JSON, so it stays on one line), for
// cachedTokenArgv.
func writeTokenAuth(c creds, argv []string) error {
	for _, v := range []string{c.ServerURL, c.Bearer, c.Project, c.Org} {
		if strings.ContainsAny(v, "\r\n") {
			return errors.New("credential value contains a line break")
		}
	}
	encodedArgv, err := json.Marshal(argv)
	if err != nil {
		return fmt.Errorf("encode token command: %w", err)
	}
	body := fmt.Sprintf("kind=%s\nserver_url=%s\nbearer=%s\nproject=%s\norg=%s\nexpires_at=%s\nargv=%s\n",
		tokenCacheKind, c.ServerURL, c.Bearer, c.Project, c.Org, c.ExpiresAt.UTC().Format(time.RFC3339Nano), encodedArgv)
	if err := atomicWriteCacheFile(authFilePath(), []byte(body)); err != nil {
		return err
	}
	markEstablished()
	return nil
}

// cachedTokenArgv returns the argv recorded with the cached token, expired or
// not: proof this box authenticates by token, used only when the device agent
// cannot be asked.
func cachedTokenArgv() ([]string, bool) {
	values, err := readAuthFile(authFilePath())
	if err != nil || values["kind"] != tokenCacheKind {
		return nil, false
	}
	var argv []string
	if json.Unmarshal([]byte(values["argv"]), &argv) != nil {
		// A token cache without a usable argv still marks a token box: the
		// source applies, and the empty argv fails as credential-unavailable.
		return nil, true
	}
	return argv, true
}

// tokenCommandEnvAllow lists the environment passed to the command: enough to
// locate the user's state and the device agent, and nothing that carries a
// credential. SPEAKEASY_* (the device agent's managed config and socket) is
// passed apart from key-shaped names.
var tokenCommandEnvAllow = map[string]bool{
	"HOME": true, "PATH": true, "USER": true, "LOGNAME": true, "TMPDIR": true,
	"XDG_CONFIG_HOME": true, "XDG_STATE_HOME": true, "XDG_RUNTIME_DIR": true,
	"SystemRoot": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "ProgramData": true,
}

func tokenCommandEnv(refresh bool) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		switch {
		case tokenCommandEnvAllow[name]:
			env = append(env, kv)
		case strings.HasPrefix(name, "SPEAKEASY_") &&
			!strings.Contains(upper, "API_KEY") && !strings.Contains(upper, "TOKEN"):
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
// debug log, masking any token-length word: a failing command must not leak a
// credential it was handling.
func sanitizeCommandStderr(raw string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(raw), "\n")
	fields := strings.Fields(line)
	for i, f := range fields {
		if len(f) >= 24 {
			fields[i] = "***"
		}
	}
	out := strings.Join(fields, " ")
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func debugTokenCommand(cfg Config, format string, args ...any) {
	debugLogf(cfg.DebugLog, format, args...)
}
