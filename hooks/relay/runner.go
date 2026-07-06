package relay

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"

	"github.com/speakeasy-api/agenthooks"
)

// authState classifies the machine's credential posture for the ratchet.
type authState int

const (
	// stateReady: a credential is available and was used for the request.
	stateReady authState = iota
	// stateNeverAuthed: no credential and the machine has never authenticated;
	// blocking paths fail open so a brand-new install cannot brick the agent.
	stateNeverAuthed
	// stateBroken: no credential but the machine authenticated before; blocking
	// paths fail closed so a wiped or revoked key cannot disable enforcement.
	stateBroken
)

// Relay translates coding-agent hook events into Gram ingest requests and
// enforces the server's verdict.
type Relay struct {
	cfg    Config
	client *client
	login  *loginFlow
}

// NewRelay builds a Relay from the resolved config.
func NewRelay(cfg Config) *Relay {
	return &Relay{
		cfg:    cfg,
		client: newClient(cfg.ServerURL),
		login:  newLoginFlow(cfg),
	}
}

// Login runs an interactive browser sign-in. force re-mints even when a
// credential is already cached. It backs the standalone login subcommand and
// the mid-session auth fallback.
func (r *Relay) Login(ctx context.Context, force bool) error {
	return r.login.Run(ctx, force)
}

// NewRunner constructs the agenthooks Runner: gating events (prompt.submitted,
// tool.requested) POST synchronously and honor deny; every other event is
// relayed as fire-and-forget telemetry. Handler failures fail open — a broken
// hook must never wedge the agent — and the credential ratchet governs the
// unauthenticated case.
func NewRunner(cfg Config) *agenthooks.Runner {
	r := NewRelay(cfg)
	runner := agenthooks.New(agenthooks.WithPolicy(agenthooks.Policy{
		Fail:            agenthooks.FailOpen,
		Unsupported:     agenthooks.Degrade,
		AskFallback:     agenthooks.FallbackNoDecision,
		ContinuationCap: 0,
		Timeout:         0,
	}))

	runner.OnPromptSubmitted(r.onPrompt)
	runner.OnToolPre(r.onToolPre)
	runner.OnPermission(r.onPermission)
	runner.OnToolPost(r.onToolPost)
	runner.OnToolError(r.onToolPost)
	runner.OnStop(r.onStop)
	runner.OnSubagentStop(r.onStop)
	runner.OnSessionStart(r.onSessionStart)
	runner.OnSessionEnd(func(ctx context.Context, e *agenthooks.SessionEndEvent) error {
		return r.onObserve(ctx, e)
	})
	runner.OnNotification(func(ctx context.Context, e *agenthooks.NotificationEvent) error {
		return r.onObserve(ctx, e)
	})
	// Cursor is the only provider whose rendered config subscribes
	// model-response natives (afterAgentResponse/afterAgentThought); they carry
	// the assistant message text and token usage.
	runner.OnModelResponse(func(ctx context.Context, e *agenthooks.ModelEvent) error {
		return r.onObserve(ctx, e)
	})

	return runner
}

// verdict is the resolved gating outcome for one event.
type verdict struct {
	block   bool
	message string
	// nudge marks a never-authenticated Claude prompt that should offer the
	// user an interactive sign-in.
	nudge bool
}

const brokenAuthMessage = "Speakeasy hooks are configured for this workspace but this machine's credentials are missing or invalid. Run the Speakeasy hooks login command to reconnect."

// deliver relays one event to the server, returning the result and the
// credential posture. It performs no work when the machine holds no credential
// so an unauthenticated install never leaks events.
func (r *Relay) deliver(ctx context.Context, typed any) (ingestResult, authState) {
	c, ok := resolveAuth(r.cfg)
	if !ok {
		if authEstablished() {
			r.debugf("event=%s no-creds state=broken authfile=%s", agenthooks.EventOf(typed).NativeName, authFilePath())
			return ingestResult{statusCode: 0, decision: decision{}, authRejected: false}, stateBroken
		}
		r.debugf("event=%s no-creds state=never-authed authfile=%s", agenthooks.EventOf(typed).NativeName, authFilePath())
		return ingestResult{statusCode: 0, decision: decision{}, authRejected: false}, stateNeverAuthed
	}

	payload := buildEnvelope(typed, hostname())
	res := r.client.send(ctx, c, payload)
	r.debugf("event=%s type=%s server=%s authfile=%s status=%d denied=%t", agenthooks.EventOf(typed).NativeName, payload.Event.Type, r.cfg.ServerURL, authFilePath(), res.statusCode, res.decision.denied())
	if res.authRejected && c.Source == credCache && !disableLocalAuth() {
		// A cache-sourced key the server rejected is forgotten; only the
		// interactive preflight/login re-mints one, so per-event senders then
		// fall through to the established (fail-closed) posture.
		forgetAuth()
		return res, stateBroken
	}
	return res, stateReady
}

// evaluate delivers a gating event and resolves the block decision under the
// ratchet and observability mode.
func (r *Relay) evaluate(ctx context.Context, typed any) verdict {
	res, state := r.deliver(ctx, typed)
	switch state {
	case stateNeverAuthed:
		return verdict{block: false, message: "", nudge: true}
	case stateBroken:
		if r.cfg.Nonblocking {
			return verdict{block: false, message: "", nudge: false}
		}
		return verdict{block: true, message: brokenAuthMessage, nudge: false}
	}

	if res.statusCode >= 200 && res.statusCode < 300 {
		if res.decision.denied() && !r.cfg.Nonblocking {
			return verdict{block: true, message: res.decision.Message, nudge: false}
		}
		return verdict{block: false, message: "", nudge: false}
	}
	// Non-2xx or unreachable: the server could not confirm the action is
	// allowed, so block unless observability mode says otherwise.
	if r.cfg.Nonblocking {
		return verdict{block: false, message: "", nudge: false}
	}
	return verdict{block: true, message: httpMessage(res), nudge: false}
}

func (r *Relay) onPrompt(ctx context.Context, e *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
	v := r.evaluate(ctx, e)
	if v.block {
		return agenthooks.BlockPrompt(v.message), nil
	}
	if v.nudge && e.Provider == agenthooks.ProviderClaudeCode {
		if note := r.loginNudge(e.Session.ID); note != "" {
			return agenthooks.AcceptPrompt().WithContext(note), nil
		}
	}
	return agenthooks.AcceptPrompt(), nil
}

func (r *Relay) onToolPre(ctx context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	v := r.evaluate(ctx, e)
	if v.block {
		// The system message mirrors the deny reason so the block text (and
		// any access-request URL in it) reaches the user verbatim, not only
		// the model.
		return agenthooks.Deny(v.message).WithSystemMessage(v.message), nil
	}
	return agenthooks.NoDecision(), nil
}

func (r *Relay) onPermission(ctx context.Context, e *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
	v := r.evaluate(ctx, e)
	if v.block {
		return agenthooks.Deny(v.message).WithSystemMessage(v.message), nil
	}
	return agenthooks.NoDecision(), nil
}

func (r *Relay) onToolPost(ctx context.Context, e *agenthooks.ToolPostEvent) (agenthooks.ToolPostDecision, error) {
	r.deliver(ctx, e)
	return agenthooks.Observed(), nil
}

func (r *Relay) onStop(ctx context.Context, e *agenthooks.StopEvent) (agenthooks.StopDecision, error) {
	r.deliver(ctx, e)
	return agenthooks.Finish(), nil
}

// onSessionStart runs the interactive sign-in preflight when the machine is not
// yet authenticated, then relays the session.started telemetry.
func (r *Relay) onSessionStart(ctx context.Context, e *agenthooks.SessionStartEvent) (agenthooks.SessionStartDecision, error) {
	if _, ok := resolveAuth(r.cfg); !ok && !authEstablished() {
		r.login.tryInteractive(ctx)
	}
	r.deliver(ctx, e)
	return agenthooks.ContinueSession(), nil
}

func (r *Relay) onObserve(ctx context.Context, typed any) error {
	r.deliver(ctx, typed)
	return nil
}

// loginNudge returns a once-per-session UserPromptSubmit additionalContext
// telling Claude the workspace is unauthenticated and how to connect, or "" if
// it has already fired this session.
func (r *Relay) loginNudge(sessionID string) string {
	if !claimNudge(sessionID) {
		return ""
	}
	return fmt.Sprintf("Notice from the Speakeasy observability plugin installed in this workspace: this machine has not completed sign-in, so the telemetry this workspace expects is not being recorded. Handle the user's current request as normal, then you MUST directly ask the user whether to connect Speakeasy hooks now — put it to them as an explicit question (use your ask-user tool if you have one); this is the user's decision, so do not resolve it yourself and do not silently drop this notice. If they agree, run: %s (opens a browser sign-in and waits for it to complete). Never run it without their explicit consent. If they decline, do not raise this again during this session.", r.loginCommand())
}

// claimNudge marks the session's nudge as emitted and reports whether this call
// won the claim (first emission wins). The marker is scoped by the credential
// cache location as well as the session: installs pointed at different auth
// files sign in independently, so one must not suppress the other's nudge.
// Installs sharing the auth file share one sign-in, so a single nudge per
// session is exactly right for them.
func claimNudge(sessionID string) bool {
	key := sessionID
	if key == "" {
		key = "session"
	}
	domain := fnv.New32a()
	_, _ = domain.Write([]byte(authFilePath()))
	marker := filepath.Join(os.TempDir(), fmt.Sprintf("speakeasy-hooks-login-nudge-%08x-%s", domain.Sum32(), sanitizeMarker(key)))
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func sanitizeMarker(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
}

// debugf appends a diagnostic line to the configured debug log, if any. It is a
// best-effort aid for local troubleshooting and never affects hook behavior.
func (r *Relay) debugf(format string, args ...any) {
	if r.cfg.DebugLog == "" {
		return
	}
	f, err := os.OpenFile(r.cfg.DebugLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	fmt.Fprintf(f, format+"\n", args...)
}

// loginCommand renders the shell command the nudge asks the agent to run to
// finish sign-in. It carries the plugin's config path so the sign-in mints a
// credential for the same server and project the hooks relay to; a bare login
// would target the production defaults and cache a key the hook path rejects.
func (r *Relay) loginCommand() string {
	cmd := "speakeasy-hooks login"
	if exe, err := os.Executable(); err == nil && exe != "" {
		cmd = shellQuoteArg(exe) + " login"
	}
	if r.cfg.ConfigPath != "" {
		cmd += " --config=" + shellQuoteArg(r.cfg.ConfigPath)
	}
	return cmd
}

// shellQuoteArg single-quotes an argument for a POSIX shell unless it is made
// entirely of characters no shell splits or interprets. The nudge command is
// executed by the agent's shell, so paths with spaces must survive parsing.
func shellQuoteArg(s string) string {
	safe := s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		case strings.ContainsRune("-_./=:@%+,", r):
			return false
		default:
			return true
		}
	})
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
