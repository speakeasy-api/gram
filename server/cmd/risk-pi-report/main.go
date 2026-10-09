// Command risk-pi-report scores the prompt-injection detector this checkout
// ships on the labelled fixtures. It writes one record per case under
// ~/.cache/gram-pi-eval/runs/<code key>, where the code key hashes the
// checkout's code outside the fixtures. A rerun resumes: it runs only new or
// edited cases and cases a previous run could not fund.
//
// `risk-pi-report view` shows every cached run and applies the merge gate to
// this checkout's run.
//
// Run it from the checkout it was built from, such as with
// `go run ./server/cmd/risk-pi-report` at the repository root.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	// defaultCorpusDir is this checkout's fixtures, relative to the repository
	// root.
	defaultCorpusDir = fixturesPath + "/prompt_injection"

	// judgeConcurrency bounds concurrent cases without turning the benchmark
	// into a provider load test.
	judgeConcurrency = 4

	// benchOrgID/benchProjectID label the judge calls. The judge needs an
	// org/project for the request shape; these are inert identifiers (the
	// dev-key provisioner ignores the org, projectID must parse as a UUID).
	benchOrgID     = "5a25158b-24dc-4d49-b03d-e85acfbea59c"
	benchProjectID = "00000000-0000-0000-0000-000000000001"

	// viewCommand names the command that shows the cached runs.
	viewCommand = "view"
)

type toolCallCase struct {
	Name string `json:"name"`
	Args string `json:"args"`
}

type labeledCase struct {
	// Window optionally supplies five-message evidence for contextual evaluation.
	Window *judgemessage.Window `json:"window,omitempty"`

	ID            string `json:"id"`
	Label         string `json:"label"`
	Text          string `json:"text"`
	Source        string `json:"source"`
	OriginalLabel string `json:"original_label,omitempty"`
	RelabelReason string `json:"relabel_reason,omitempty"`
	// Optional agent-runtime framing: plain rows omit these (judged as end-user
	// content); typed rows carry the message type + tool the judge uses.
	Type                   string         `json:"type,omitempty"`       // message.Type; default user_message
	Tool                   string         `json:"tool,omitempty"`       // tool name for a single-tool tool_request/tool_response
	ToolCalls              []toolCallCase `json:"tool_calls,omitempty"` // multi-call tool_request
	PriorUserRequest       string         `json:"prior_user_request,omitempty"`
	RecentUntrustedContent string         `json:"recent_untrusted_content,omitempty"`

	// WellKnown names the classic phrase of a well-known attack, such as
	// "Ignore previous instructions"; empty for every other case.
	WellKnown string `json:"well_known,omitempty"`

	// raw is the fixture line the case was read from. Hashing it identifies
	// the case the same way across versions of this tool.
	raw string
}

func (c labeledCase) trajectory() judgemessage.Trajectory {
	return judgemessage.Trajectory{
		PriorUserRequest:       c.PriorUserRequest,
		RecentUntrustedContent: c.RecentUntrustedContent,
	}
}

// caseType returns the message type for a case, defaulting to user_message.
func (c labeledCase) caseType() message.Type {
	if c.Type == "" {
		return message.User
	}
	return c.Type
}

// judgeMessage renders a case as the judgemessage the judge evaluates,
// preserving its agent-runtime framing (produced_by/body_kind/tool).
func (c labeledCase) judgeMessage() judgemessage.Message {
	if len(c.ToolCalls) > 0 {
		calls := make([]judgemessage.ToolCall, len(c.ToolCalls))
		for i, tc := range c.ToolCalls {
			calls[i] = judgemessage.NewToolCall(tc.Name, tc.Args)
		}
		return judgemessage.NewForToolCalls(calls)
	}
	return judgemessage.New(c.caseType(), c.Tool, c.Text)
}

func main() {
	if err := dispatch(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "risk-pi-report: %v\n", err)
		os.Exit(1)
	}
}

// dispatch runs the view command, or measures this checkout's detector.
func dispatch(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == viewCommand {
		return runView(ctx, args[1:])
	}
	return runMeasure(ctx, args)
}

// runMeasure scores this checkout's detector. Every commit's build of this
// tool is run this way, so the call takes no required flag.
func runMeasure(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("risk-pi-report", flag.ExitOnError)
	corpusDir := flags.String("corpus-dir", defaultCorpusDir, "directory of prompt-injection JSONL fixtures to score; defaults to this checkout's")
	_ = flags.Parse(args) // ExitOnError exits on a bad flag.
	// A command this version does not know must not start a paid run.
	if flags.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "risk-pi-report: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		os.Exit(2)
	}
	corpus, err := loadCorpus(*corpusDir)
	if err != nil {
		return err
	}
	code, err := measuredCode(ctx, "")
	if err != nil {
		return err
	}
	runs, err := runsDir()
	if err != nil {
		return err
	}
	return runRecords(ctx, options{runDir: filepath.Join(runs, code.key), commit: code.commit}, corpus)
}

// corpusOrder lists the fixture files that load first, in this order, so
// text they share dedupes as it always has. Every other *.jsonl file in the
// corpus directory loads after them, in name order, so a version of this tool
// scores fixture files added after it.
var corpusOrder = []string{
	"deepset.jsonl",
	"gram_benigns.jsonl",
	"litellm_extended.jsonl",
	"mutations.jsonl",
	"operational_benigns.jsonl",
	"agent_fp_benigns.jsonl",
	"adversarial_fable.jsonl",
	"adversarial_codex.jsonl",
	"agent_fp_ais324.jsonl",
	"adversarial_ais324.jsonl",
	"trajectory_twins.jsonl",
	"llmail_inject.jsonl",
	"agentdojo.jsonl",
	"agentdyn.jsonl",
}

// repeatedTextFiles hold cases that share text on purpose: paired trajectory
// rows and AgentDojo's task occurrences carry different context. Their cases
// are never deduped.
var repeatedTextFiles = []string{"trajectory_twins.jsonl", "agentdojo.jsonl"}

// corpusFiles lists dir's fixture files in load order.
func corpusFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list corpus files: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			paths = append(paths, filepath.Join(dir, entry.Name()))
		}
	}
	rank := func(path string) int {
		if i := slices.Index(corpusOrder, filepath.Base(path)); i >= 0 {
			return i
		}
		return len(corpusOrder)
	}
	slices.SortStableFunc(paths, func(a, b string) int { return rank(a) - rank(b) })
	return paths, nil
}

func loadCorpus(dir string) ([]labeledCase, error) {
	seen := map[string]string{}
	var out []labeledCase

	load := func(path string, dedupe bool) error {
		name := filepath.Base(path)
		f, err := os.Open(path) // #nosec G304 -- local developer/CI harness intentionally reads a configured corpus path.
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		defer func() { _ = f.Close() }()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			raw := strings.TrimSpace(scanner.Text())
			if raw == "" {
				continue
			}
			var c labeledCase
			if err := json.Unmarshal([]byte(raw), &c); err != nil {
				return fmt.Errorf("%s line %d unmarshal: %w", name, line, err)
			}
			c.raw = raw
			if c.ID == "" {
				return fmt.Errorf("%s line %d missing id", name, line)
			}
			if c.Label != "malicious" && c.Label != "benign" {
				return fmt.Errorf("%s line %d invalid label %q", name, line, c.Label)
			}
			if c.Type != "" && !message.IsTypeValid(c.Type) {
				return fmt.Errorf("%s line %d invalid type %q", name, line, c.Type)
			}
			if dedupe {
				if _, dup := seen[c.Text]; dup {
					continue
				}
				seen[c.Text] = c.ID
			}
			out = append(out, c)
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("scan %s: %w", path, err)
		}
		return nil
	}

	paths, err := corpusFiles(dir)
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		dedupe := !slices.Contains(repeatedTextFiles, filepath.Base(path))
		if err := load(path, dedupe); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no cases in %s", dir)
	}
	return out, nil
}

// newOpenRouterClient builds the real production OpenRouter client with the
// org-scoped concerns stubbed: a dev-key provisioner, and nil capture/usage/
// title/telemetry strategies (all nil-guarded). Same construction as
// riskjudgebench, so the bench runs under prod-equivalent conditions.
func newOpenRouterClient(apiKey string) openrouter.CompletionClient {
	logger := slog.New(slog.DiscardHandler)
	policy := guardian.NewDefaultPolicy(tracenoop.NewTracerProvider())
	prov := &devProvisioner{apiKey: apiKey}
	return openrouter.NewUnifiedClient(
		logger,
		policy,
		prov,
		&openrouter.PlatformKeyResolver{Provisioner: prov},
		nil, // message capture  (nil-guarded)
		nil, // usage tracking   (nil-guarded)
		nil, // chat title gen   (nil-guarded)
		nil, // telemetry logger (nil-guarded)
	)
}

// devProvisioner satisfies openrouter.Provisioner but skips the DB/billing path:
// it hands back the dev key for every org.
type devProvisioner struct{ apiKey string }

func (d *devProvisioner) ProvisionAPIKey(_ context.Context, _ string, _ openrouter.KeyType) (string, error) {
	return d.apiKey, nil
}
func (d *devProvisioner) RefreshAPIKeyLimit(_ context.Context, _ string, _ openrouter.KeyType, _ *int) (int, error) {
	return 0, fmt.Errorf("not implemented in bench")
}
func (*devProvisioner) AddAPIKeyDisableCause(context.Context, string, openrouter.KeyType, openrouter.DisableCause) (openrouter.DisableCauseChange, error) {
	return openrouter.DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}, nil
}

func (*devProvisioner) RemoveAPIKeyDisableCause(context.Context, string, openrouter.KeyType, openrouter.DisableCause, *int) (int, openrouter.DisableCauseChange, error) {
	return 0, openrouter.DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}, nil
}

func (d *devProvisioner) DisableAPIKey(_ context.Context, _ string, _ openrouter.KeyType) error {
	return fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) GetCreditsUsed(_ context.Context, _ string, _ openrouter.KeyType) (float64, int, error) {
	return 0, 0, fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) GetKeyUsage(_ context.Context, _ string) (float64, *int64, error) {
	return 0, nil, fmt.Errorf("not implemented in bench")
}
func (d *devProvisioner) ReconcileMonthlyCredits(_ context.Context, _ string, _ openrouter.KeyType, currentLimit int64, _ int64, _ *int64) (int64, error) {
	return currentLimit, nil
}
func (d *devProvisioner) GetModelUsage(_ context.Context, _ string, _ string, _ openrouter.KeyType) (*openrouter.ModelUsage, error) {
	return nil, fmt.Errorf("not implemented in bench")
}

var _ openrouter.Provisioner = (*devProvisioner)(nil)

// unsetEnvPlaceholder is mise.toml's OPENROUTER_DEV_KEY. Under mise it
// overrides the process environment, so CI passes its key as
// OPENROUTER_API_KEY and the placeholder must not shadow it.
const unsetEnvPlaceholder = "unset"

// firstEnv returns the first variable set to a value other than empty or
// the mise placeholder.
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" && v != unsetEnvPlaceholder {
			return v
		}
	}
	return ""
}
