package relay

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
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
	// stateReauthNeeded: a cached key was rejected by the server and cleared.
	// Tool events still fail closed, but prompt submissions nudge the user to
	// reconnect instead of blocking every turn on a credential that is gone.
	stateReauthNeeded
)

// Relay translates coding-agent hook events into Gram ingest requests and
// enforces the server's verdict.
type Relay struct {
	cfg    Config
	client *client
	login  *loginFlow
	// backfillDeny carries a blocked verdict from a reporting-only backfilled
	// prompt (agenthooks discards its decision) to the decision-capable event
	// that triggered the backfill in this same process. It was the turn's
	// only prompt-policy check, so the deny gates that event instead.
	backfillDeny string
}

// NewRelay builds a Relay from the resolved config.
func NewRelay(cfg Config) *Relay {
	return &Relay{
		cfg:          cfg,
		client:       newClient(cfg.ServerURL),
		login:        newLoginFlow(cfg),
		backfillDeny: "",
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

const reauthNeededMessage = "Speakeasy hooks need to reconnect. Run the Speakeasy hooks login command to reconnect."

const envKeyRejectedMessage = "Speakeasy hooks rejected the API key configured in GRAM_HOOKS_API_KEY. Update or unset GRAM_HOOKS_API_KEY, then run the Speakeasy hooks login command to reconnect."

// deliver relays one event to the server, returning the result and the
// credential posture. It performs no work when the machine holds no credential
// so an unauthenticated install never leaks events.
func (r *Relay) deliver(ctx context.Context, typed any) (ingestResult, authState) {
	// An unreadable speakeasy.json means the deployment identity is unknown:
	// the default server plus a cached key would route this workspace's events
	// to whatever project the cache was minted for. Skip the network with the
	// ratchet's usual split instead.
	if r.cfg.ConfigError != "" {
		r.debugf("event=%s config-error path=%s err=%s", agenthooks.EventOf(typed).NativeName, r.cfg.ConfigPath, r.cfg.ConfigError)
		if authEstablished() {
			msg := fmt.Sprintf("Speakeasy hooks cannot read the plugin config at %q. Reinstall the Speakeasy hooks plugin.", r.cfg.ConfigPath)
			return ingestResult{statusCode: 0, decision: decision{Decision: "", Reason: "", Message: msg}, authRejected: false}, stateBroken
		}
		return ingestResult{statusCode: 0, decision: decision{}, authRejected: false}, stateNeverAuthed
	}

	// Refuse to send credentials over plaintext HTTP before resolving them,
	// with the ratchet's usual split: never-authenticated machines skip the
	// network silently, established machines fail closed.
	if insecureServerURL(r.cfg.ServerURL) {
		r.debugf("event=%s insecure-server-url server=%s", agenthooks.EventOf(typed).NativeName, r.cfg.ServerURL)
		if authEstablished() {
			msg := fmt.Sprintf("Speakeasy hooks refused insecure Gram server URL %q; use https:// (or an http://localhost dev server).", r.cfg.ServerURL)
			return ingestResult{statusCode: 0, decision: decision{Decision: "", Reason: "", Message: msg}, authRejected: false}, stateBroken
		}
		return ingestResult{statusCode: 0, decision: decision{}, authRejected: false}, stateNeverAuthed
	}

	c, ok := resolveAuth(r.cfg)
	if !ok {
		if reauthNeeded() {
			r.debugf("event=%s no-creds state=reauth-needed authfile=%s", agenthooks.EventOf(typed).NativeName, authFilePath())
			return ingestResult{statusCode: 0, decision: decision{}, authRejected: false}, stateReauthNeeded
		}
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
	if res.authRejected && c.Source == credEnv {
		// The configured key is authoritative and a re-login can never replace
		// it, so name it in the failure instead of pointing at the cache flow.
		if msg := strings.TrimSpace(res.decision.Message); msg != "" {
			res.decision.Message = envKeyRejectedMessage + " Server response: " + msg
		} else {
			res.decision.Message = envKeyRejectedMessage
		}
		return res, stateReady
	}
	if res.authRejected && c.Source == credCache && !disableLocalAuth() {
		// A cache-sourced key the server rejected is forgotten; only the
		// interactive preflight/login re-mints one. The reauth marker keeps
		// prompt submissions nudging reconnect instead of blocking on it.
		forgetAuth()
		markReauthNeeded()
		return res, stateReauthNeeded
	}
	return res, stateReady
}

// evaluate delivers a gating event and resolves the block decision under the
// ratchet and observability mode.
func (r *Relay) evaluate(ctx context.Context, typed any) verdict {
	res, state := r.deliver(ctx, typed)
	switch state {
	case stateNeverAuthed:
		// A broken config suppresses the nudge: sign-in cannot recover an
		// unknown deployment identity, only a reinstall can.
		return verdict{block: false, message: "", nudge: r.cfg.ConfigError == ""}
	case stateBroken:
		if r.cfg.Nonblocking {
			return verdict{block: false, message: "", nudge: false}
		}
		msg := res.decision.Message
		if msg == "" {
			msg = brokenAuthMessage
		}
		return verdict{block: true, message: msg, nudge: false}
	case stateReauthNeeded:
		if r.cfg.Nonblocking {
			return verdict{block: false, message: "", nudge: false}
		}
		// Tool events fail closed on the rejection (or its memory); prompt
		// handlers honor nudge and fail open instead.
		msg := reauthNeededMessage
		if res.statusCode != 0 {
			msg = httpMessage(res)
		}
		return verdict{block: true, message: msg, nudge: true}
	}

	if res.statusCode >= 200 && res.statusCode < 300 {
		switch {
		case strings.EqualFold(res.decision.Decision, "allow"):
			return verdict{block: false, message: "", nudge: false}
		case r.cfg.Nonblocking:
			return verdict{block: false, message: "", nudge: false}
		case res.decision.denied():
			return verdict{block: true, message: res.decision.Message, nudge: false}
		default:
			// A 2xx whose body carries no explicit verdict (an intermediary's
			// JSON, a skewed server) is not an allow.
			msg := strings.TrimSpace(res.decision.Message)
			if msg == "" {
				msg = "Speakeasy hooks could not read the server's verdict."
			}
			return verdict{block: true, message: msg, nudge: false}
		}
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
	if e.Backfilled {
		if v.block {
			r.backfillDeny = v.message
			if r.backfillDeny == "" {
				r.backfillDeny = "Speakeasy blocked this turn's prompt."
			}
		}
		return agenthooks.AcceptPrompt(), nil
	}
	// A nudge-worthy posture (never authenticated, or a cleared rejected key)
	// fails the prompt open on every provider: blocking each turn on a
	// credential the machine does not hold would brick the session, and the
	// turn's tool events stay gated regardless. Only Claude can carry the
	// context note that routes the user to sign-in.
	if v.nudge {
		if e.Provider == agenthooks.ProviderClaudeCode {
			if note := r.loginNudge(e.Session.ID); note != "" {
				return agenthooks.AcceptPrompt().WithContext(note), nil
			}
		}
		return agenthooks.AcceptPrompt(), nil
	}
	if v.block {
		return agenthooks.BlockPrompt(v.message), nil
	}
	return agenthooks.AcceptPrompt(), nil
}

func (r *Relay) onToolPre(ctx context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
	// A denied backfilled prompt would have blocked at prompt submission had
	// that delivery not been missed; the deny lands on this triggering event
	// instead, without reporting the tool call the agent never got to make.
	if msg := r.backfillDeny; msg != "" {
		r.backfillDeny = ""
		return agenthooks.Deny(msg).WithSystemMessage(msg), nil
	}
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
	if msg := r.backfillDeny; msg != "" {
		r.backfillDeny = ""
		return agenthooks.Deny(msg).WithSystemMessage(msg), nil
	}
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

// onSessionStart runs the interactive sign-in preflight whenever the machine
// holds no usable credential — never authenticated, key lost or rejected, or a
// shared cache minted for a different server or org — then relays the
// session.started telemetry. Viability guards and the attempt cooldown keep
// the browser from nagging.
func (r *Relay) onSessionStart(ctx context.Context, e *agenthooks.SessionStartEvent) (agenthooks.SessionStartDecision, error) {
	if _, ok := resolveAuth(r.cfg); !ok {
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

// shellQuoteArg quotes an argument for the agent's shell unless it is made
// entirely of characters no shell splits or interprets. The nudge command is
// executed by the agent, so paths with spaces must survive parsing. POSIX
// shells get single quotes; Windows shells (cmd.exe, PowerShell) don't group
// on single quotes, so they get double quotes — safe unescaped because NTFS
// forbids '"' in paths.
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
	if runtime.GOOS == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
