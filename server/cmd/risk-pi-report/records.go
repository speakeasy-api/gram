package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// caseStatus is a case's outcome in a run record.
type caseStatus string

const (
	// statusFlagged means the cascade reported prompt injection.
	statusFlagged caseStatus = "flagged"

	// statusClear means the cascade reached a verdict of no prompt injection.
	statusClear caseStatus = "clear"

	// statusNoVerdict means the cascade reached no verdict: both models
	// refused, the verdict was malformed, the evidence was too large, or a
	// provider failed. It scores as a miss for an attack.
	statusNoVerdict caseStatus = "no_verdict"

	// statusOutOfCredit means OpenRouter rejected a call for lack of credit.
	// The next run redoes the case once the balance covers it.
	statusOutOfCredit caseStatus = "out_of_credit"
)

const (
	// casesFile and manifestFile name a run directory's per-case records and
	// its metadata.
	casesFile    = "cases.jsonl"
	manifestFile = "run.json"

	// caseHashHexLen is the length of a case's content hash. 64 bits tells
	// apart the edits of a corpus of a few thousand cases.
	caseHashHexLen = 16

	// maxDetailRunes bounds the rationale or error kept per case, so a record
	// stays one short line.
	maxDetailRunes = 300

	// progressEvery is how many finished cases pass between progress lines.
	progressEvery = 50

	// costPerCaseUSD estimates one case's OpenRouter cost for the credit
	// check: a full run of 2,046 cases costs about $2.30, and the margin
	// covers the credit OpenRouter reserves for calls in flight.
	costPerCaseUSD = 0.0015

	// creditHeadroomUSD is balance kept beyond a run's expected cost.
	// OpenRouter holds each in-flight call's maximum cost against the balance,
	// so calls fail with 402 below about $1 even when the expected cost fits.
	creditHeadroomUSD = 1.0

	// creditCheckTimeout bounds each OpenRouter credit request.
	creditCheckTimeout = 10 * time.Second
)

// caseRecord is one case's result in a run, written to cases.jsonl as soon as
// the case finishes.
type caseRecord struct {
	// Key is the case's source and id, "<source>::<id>".
	Key string `json:"key"`

	// Hash identifies the case content. Editing a fixture changes it, so the
	// edited case runs again.
	Hash string `json:"hash"`

	// Status is the case outcome.
	Status caseStatus `json:"status"`

	// Model is the model whose verdict decided the case: Jev when it cleared
	// the case, otherwise the confirmer.
	Model string `json:"model,omitempty"`

	// Detail is the deciding model's rationale, or why no verdict was reached.
	Detail string `json:"detail,omitempty"`

	// Refused reports that the confirmation model refused the case, which
	// leaves it without a verdict.
	Refused bool `json:"refused,omitempty"`

	// CostUSD is the provider-reported cost of the case's calls.
	CostUSD float64 `json:"cost_usd"`

	// LatencyMS is the case's total time in milliseconds, retries included.
	LatencyMS float64 `json:"latency_ms"`
}

// runManifest describes a run directory: which code produced it and with
// which models and prompts.
type runManifest struct {
	// EvaluatorSHA256 binds records to the compiled evaluator and its runtime settings.
	EvaluatorSHA256 string `json:"evaluator_sha256"`

	// Label names the side of a comparison, "main" or "this change".
	Label string `json:"label"`

	// Ref names the code, such as "fix/branch @ 1a2b3c4d5e + uncommitted".
	Ref string `json:"ref"`

	// PrefilterModel and PrefilterThreshold describe the Jev stage.
	PrefilterModel string `json:"prefilter_model"`

	// PrefilterThreshold is the Jev probability that escalates a case.
	PrefilterThreshold float64 `json:"prefilter_threshold"`

	// ConfirmationModel confirms Jev's candidates.
	ConfirmationModel string `json:"confirmation_model"`

	// RefusalFallbackModel preserves historical run metadata for display only.
	// Current runs do not use a refusal fallback.
	RefusalFallbackModel string `json:"refusal_fallback_model,omitempty"`

	// ConfirmationPromptSHA256 hashes the confirmer prompt.
	ConfirmationPromptSHA256 string `json:"confirmation_prompt_sha256"`

	// PrefilterQuestionsSHA256 hashes Jev's questions.
	PrefilterQuestionsSHA256 string `json:"prefilter_questions_sha256"`

	// Updated is when the run last wrote a record.
	Updated time.Time `json:"updated"`
}

// sideTotals summarizes a run over a corpus. Pending cases have no record for
// their current content yet.
type sideTotals struct {
	// Cases counts the corpus cases.
	Cases int `json:"cases"`

	// Benign counts benign cases; FalsePositives counts those flagged.
	Benign int `json:"benign"`

	// FalsePositives counts benign cases flagged.
	FalsePositives int `json:"false_positives"`

	// Attacks counts malicious cases; Caught counts those flagged.
	Attacks int `json:"attacks"`

	// Caught counts malicious cases flagged.
	Caught int `json:"caught"`

	// WellKnown counts malicious cases tagged well_known.
	WellKnown int `json:"well_known"`

	// WellKnownCaught counts well-known attacks flagged.
	WellKnownCaught int `json:"well_known_caught"`

	// Refused counts cases the confirmation model refused.
	Refused int `json:"refused"`

	// NoVerdict counts cases with no verdict.
	NoVerdict int `json:"no_verdict"`

	// OutOfCredit counts cases OpenRouter rejected for lack of credit.
	OutOfCredit int `json:"out_of_credit"`

	// Pending counts cases with no record yet.
	Pending int `json:"pending"`

	// CostUSD sums the recorded cases' cost.
	CostUSD float64 `json:"cost_usd"`

	// LatencyP50MS and LatencyP90MS summarize decision time over recorded cases.
	LatencyP50MS float64 `json:"latency_p50_ms"`

	// LatencyP90MS is the 90th percentile decision time.
	LatencyP90MS float64 `json:"latency_p90_ms"`
}

// caseKey identifies a case across runs.
func caseKey(c labeledCase) string {
	return c.Source + "::" + c.ID
}

// caseHash fingerprints a case's content, so an edited fixture runs again.
func caseHash(c labeledCase) string {
	raw, err := json.Marshal(c)
	if err != nil {
		// labeledCase holds only strings, bools and slices of them, which
		// always marshal; fall back to the key so a case still has a hash.
		raw = []byte(caseKey(c))
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum)[:caseHashHexLen]
}

// isOutOfCredit reports whether a call failed because the OpenRouter balance
// cannot fund it.
func isOutOfCredit(err error) bool {
	if openrouter.IsInsufficientCredits(err) {
		return true
	}
	if status, ok := errors.AsType[*openrouter.HTTPError](err); ok && status.StatusCode == http.StatusPaymentRequired {
		return true
	}
	status, ok := errors.AsType[*typesafe.StatusError](err)
	return ok && status.StatusCode == http.StatusPaymentRequired
}

// caseOutcome is what the cascade produced for one case.
type caseOutcome struct {
	findings    []scanners.Finding
	verdict     promptinjection.Result
	err         error
	observation decisionObservation
	refused     bool
}

// recordFromOutcome turns a finished case into its run record.
func recordFromOutcome(c labeledCase, o caseOutcome) caseRecord {
	rec := caseRecord{
		Key:       caseKey(c),
		Hash:      caseHash(c),
		Status:    statusClear,
		Model:     o.verdict.Model,
		Detail:    truncateRunes(o.verdict.Rationale, maxDetailRunes),
		Refused:   o.refused,
		CostUSD:   0,
		LatencyMS: float64(o.observation.Latency) / float64(time.Millisecond),
	}
	outOfCredit := isOutOfCredit(o.err)
	var lastErr error
	for _, call := range o.observation.Calls {
		rec.CostUSD += call.CostUSD
		if call.Err != nil {
			lastErr = call.Err
			outOfCredit = outOfCredit || isOutOfCredit(call.Err)
		}
	}
	switch {
	case outOfCredit:
		rec.Status = statusOutOfCredit
		rec.Detail = "OpenRouter returned 402: out of credit"
	case o.err != nil || o.verdict.Label == promptinjection.LabelUnavailable:
		rec.Status = statusNoVerdict
		rec.Detail = noVerdictDetail(o.refused, lastErr, o.err)
	case len(o.findings) > 0:
		rec.Status = statusFlagged
	}
	return rec
}

// noVerdictDetail says why a case reached no verdict.
func noVerdictDetail(refused bool, lastCallErr, caseErr error) string {
	if refused {
		return "refused by the confirmation model"
	}
	if lastCallErr != nil && !errors.Is(lastCallErr, promptinjection.ErrNoVerdict) {
		return truncateRunes(lastCallErr.Error(), maxDetailRunes)
	}
	if caseErr != nil {
		return truncateRunes(caseErr.Error(), maxDetailRunes)
	}
	return "no verdict"
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// loadRecords reads a run's records by case key; a later line replaces an
// earlier one. A missing file is an empty run. A malformed line, such as a
// write cut short by a crash, is skipped.
func loadRecords(path string) (map[string]caseRecord, error) {
	out := map[string]caseRecord{}
	f, err := os.Open(path) // #nosec G304 -- the run directory is a developer-chosen CLI path.
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open run records: %w", err)
	}
	defer o11y.NoLogDefer(f.Close)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var rec caseRecord
		if json.Unmarshal(scanner.Bytes(), &rec) != nil || rec.Key == "" {
			continue
		}
		out[rec.Key] = rec
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read run records: %w", err)
	}
	return out, nil
}

// currentRecord returns the case's record when it matches the case's current
// content.
func currentRecord(records map[string]caseRecord, c labeledCase) (caseRecord, bool) {
	rec, ok := records[caseKey(c)]
	if !ok || rec.Hash != caseHash(c) {
		var none caseRecord
		return none, false
	}
	return rec, true
}

// casesToRun lists the cases without a usable record: new or edited cases,
// and cases a previous run could not fund.
func casesToRun(corpus []labeledCase, records map[string]caseRecord) []labeledCase {
	var todo []labeledCase
	for _, c := range corpus {
		rec, ok := currentRecord(records, c)
		if !ok || rec.Status == statusOutOfCredit {
			todo = append(todo, c)
		}
	}
	return todo
}

// computeTotals summarizes a run over the corpus.
func computeTotals(corpus []labeledCase, records map[string]caseRecord) sideTotals {
	var t sideTotals
	var latencies []float64
	for _, c := range corpus {
		t.Cases++
		malicious := c.Label == "malicious"
		if malicious {
			t.Attacks++
		}
		if malicious && c.WellKnown != "" {
			t.WellKnown++
		}
		if !malicious {
			t.Benign++
		}
		rec, ok := currentRecord(records, c)
		if !ok {
			t.Pending++
			continue
		}
		t.CostUSD += rec.CostUSD
		if rec.Refused {
			t.Refused++
		}
		switch rec.Status {
		case statusOutOfCredit:
			t.OutOfCredit++
			continue
		case statusNoVerdict:
			t.NoVerdict++
		case statusFlagged:
			t.FalsePositives += boolInt(!malicious)
			t.Caught += boolInt(malicious)
			t.WellKnownCaught += boolInt(malicious && c.WellKnown != "")
		case statusClear:
		}
		latencies = append(latencies, rec.LatencyMS)
	}
	t.LatencyP50MS = percentile(latencies, 0.5)
	t.LatencyP90MS = percentile(latencies, 0.9)
	return t
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// percentile returns the nearest-rank percentile of values, or 0 for none.
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[max(rank, 0)]
}

// gateTallyFromRecords scores the merge gate from a run's records.
func gateTallyFromRecords(corpus []labeledCase, records map[string]caseRecord) gateTally {
	var tally gateTally
	for _, c := range corpus {
		rec, _ := currentRecord(records, c)
		flagged := rec.Status == statusFlagged
		if c.Label == "benign" && flagged {
			tally.FalsePositives++
			tally.FalsePositiveKeys = append(tally.FalsePositiveKeys, caseKey(c))
		}
		if c.Label != "malicious" {
			continue
		}
		tally.Attacks++
		if flagged {
			tally.AttacksCaught++
		}
	}
	return tally
}

// formatTotals renders a run's totals as one progress or summary line.
func formatTotals(label string, t sideTotals) string {
	done := t.Cases - t.Pending - t.OutOfCredit
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d/%d cases · FP %d · caught %d/%d", label, done, t.Cases, t.FalsePositives, t.Caught, t.Attacks)
	if t.WellKnown > 0 {
		fmt.Fprintf(&b, " · well-known %d/%d", t.WellKnownCaught, t.WellKnown)
	}
	fmt.Fprintf(&b, " · refused %d · no verdict %d · out of credit %d · $%.2f", t.Refused, t.NoVerdict, t.OutOfCredit, t.CostUSD)
	return b.String()
}

// runRecorder appends records to a run directory and prints progress.
type runRecorder struct {
	mu      sync.Mutex
	file    *os.File
	corpus  []labeledCase
	records map[string]caseRecord
	label   string
	done    int
}

func (r *runRecorder) add(rec caseRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal run record: %w", err)
	}
	// Separate an interrupted final line before appending this run's first record.
	// Empty lines are ignored by the loader.
	if r.done == 0 {
		line = append([]byte{'\n'}, line...)
	}
	if _, err := r.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append run record: %w", err)
	}
	r.records[rec.Key] = rec
	r.done++
	if r.done%progressEvery == 0 {
		fmt.Fprintln(os.Stderr, formatTotals(r.label, computeTotals(r.corpus, r.records)))
	}
	return nil
}

// runRecords evaluates the cascade on the cases a run directory lacks, writing
// each record as it finishes, then summarizes the whole run and applies the
// merge gate when one is set. An out-of-credit run stops at once and keeps its
// records, so the next run continues where it stopped.
func runRecords(ctx context.Context, opts options, corpus []labeledCase) error {
	if err := os.MkdirAll(opts.runDir, 0o750); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}
	casesPath := filepath.Join(opts.runDir, casesFile)
	records, err := loadRecords(casesPath)
	if err != nil {
		return err
	}
	manifest, err := currentManifest(opts)
	if err != nil {
		return err
	}
	previous, err := loadManifest(opts.runDir)
	if err != nil {
		return err
	}
	if len(records) > 0 && (previous.EvaluatorSHA256 == "" || previous.EvaluatorSHA256 != manifest.EvaluatorSHA256) {
		return fmt.Errorf("run directory contains records from an incompatible evaluator; choose a new --run-dir")
	}
	// Publish the identity before any case is appended, including interrupted runs.
	if err := writeManifest(opts.runDir, manifest); err != nil {
		return err
	}
	todo := casesToRun(corpus, records)
	fmt.Fprintf(os.Stderr, "%s: %d cases, %d reused, %d to run (%s)\n", opts.label, len(corpus), len(corpus)-len(todo), len(todo), opts.ref)
	stopped := false
	if len(todo) > 0 {
		key := firstEnv("OPENROUTER_DEV_KEY", "OPENROUTER_API_KEY")
		if key == "" {
			return fmt.Errorf("set OPENROUTER_DEV_KEY or OPENROUTER_API_KEY")
		}
		client := guardian.NewDefaultPolicy(tracenoop.NewTracerProvider()).PooledClient()
		if err := checkCredit(ctx, client, openrouter.OpenRouterBaseURL, key, len(todo)); err != nil {
			return err
		}
		stopped, err = evaluateIntoRun(ctx, opts, key, corpus, todo, records, casesPath)
		if err != nil {
			return err
		}
	}
	manifest.Updated = time.Now().UTC()
	if err := writeManifest(opts.runDir, manifest); err != nil {
		return err
	}
	return finishRun(opts, corpus, records, stopped)
}

// finishRun prints the run's totals and applies the merge gate when one is
// set. A run that ran out of credit, or still has cases without a result,
// cannot pass the gate.
func finishRun(opts options, corpus []labeledCase, records map[string]caseRecord, stopped bool) error {
	totals := computeTotals(corpus, records)
	fmt.Fprintln(os.Stderr, formatTotals(opts.label, totals))
	if stopped || totals.OutOfCredit > 0 {
		return fmt.Errorf("%s: out of OpenRouter credit with %d cases left; add account credit at https://openrouter.ai/settings/credits or raise the key spending limit as appropriate, then rerun to continue", opts.label, totals.Pending+totals.OutOfCredit)
	}
	if opts.maxFalsePositives == gateDisabledFalsePositives && opts.minRecall == 0 {
		return nil
	}
	if totals.Pending > 0 {
		return fmt.Errorf("%s: %d cases have no result; rerun to finish before the merge gate", opts.label, totals.Pending)
	}
	gate, gateErr := evaluateGate(opts.maxFalsePositives, opts.minRecall, []gateTally{gateTallyFromRecords(corpus, records)})
	printGate(os.Stderr, gate)
	return gateErr
}

// evaluateIntoRun runs the cascade on todo, appending each finished case to
// the run. It reports whether the run stopped for lack of credit.
func evaluateIntoRun(ctx context.Context, opts options, key string, corpus, todo []labeledCase, records map[string]caseRecord, casesPath string) (bool, error) {
	file, err := os.OpenFile(casesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- the run directory is a developer-chosen CLI path.
	if err != nil {
		return false, fmt.Errorf("open run records for append: %w", err)
	}
	defer o11y.NoLogDefer(file.Close)
	recorder := &runRecorder{mu: sync.Mutex{}, file: file, corpus: corpus, records: records, label: opts.label, done: 0}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		stopMu  sync.Mutex
		stopped bool
		addErr  error
	)
	onCase := func(i int, o caseOutcome) {
		rec := recordFromOutcome(todo[i], o)
		stopMu.Lock()
		defer stopMu.Unlock()
		// After a stop, calls cut short by the cancellation leave the case
		// for the next run rather than recording a failure it did not have.
		if runCtx.Err() != nil && rec.Status == statusNoVerdict {
			return
		}
		if rec.Status == statusOutOfCredit && !stopped {
			stopped = true
			cancel()
		}
		addErr = errors.Join(addErr, recorder.add(rec))
	}
	if _, _, err := scanCascade(runCtx, opts, key, todo, onCase); err != nil {
		if !stopped || ctx.Err() != nil || !errors.Is(err, context.Canceled) {
			return stopped, errors.Join(addErr, err)
		}
	}
	return stopped, addErr
}

// currentManifest identifies the actual executable, not a user-supplied ref.
// Rebuilding with changed evaluator code, dependencies, or prompts invalidates
// reuse, including uncommitted edits. Conservative rebuild invalidation is safe.
func currentManifest(opts options) (runManifest, error) {
	var manifest runManifest
	executable, err := os.Executable()
	if err != nil {
		return manifest, fmt.Errorf("locate evaluator executable: %w", err)
	}
	file, err := os.Open(executable) // #nosec G304 -- os.Executable identifies this running binary.
	if err != nil {
		return manifest, fmt.Errorf("open evaluator executable: %w", err)
	}
	defer o11y.NoLogDefer(file.Close)
	code := sha256.New()
	if _, err := io.Copy(code, file); err != nil {
		return manifest, fmt.Errorf("hash evaluator executable: %w", err)
	}

	confirmationHash, questionsHash, err := registryPromptHashes()
	if err != nil {
		return manifest, err
	}
	manifest = runManifest{
		EvaluatorSHA256:          "",
		Label:                    opts.label,
		Ref:                      opts.ref,
		PrefilterModel:           typesafe.Model,
		PrefilterThreshold:       piopenrouter.PrefilterThreshold,
		ConfirmationModel:        piopenrouter.ConfirmationModel,
		RefusalFallbackModel:     "",
		ConfirmationPromptSHA256: confirmationHash,
		PrefilterQuestionsSHA256: questionsHash,
		Updated:                  time.Now().UTC(),
	}
	manifest.EvaluatorSHA256, err = evaluatorFingerprint(manifest, fmt.Sprintf("%x", code.Sum(nil)), opts)
	return manifest, err
}

// evaluatorFingerprint excludes display metadata and gate settings, which do
// not change a case verdict. Case content is fingerprinted separately.
func evaluatorFingerprint(manifest runManifest, code string, opts options) (string, error) {
	manifest.EvaluatorSHA256, manifest.Label, manifest.Ref = "", "", ""
	manifest.Updated = time.Time{}
	configuration, err := json.Marshal(struct {
		Code        string      `json:"code"`
		Manifest    runManifest `json:"manifest"`
		Reasoning   string      `json:"reasoning"`
		Concurrency int         `json:"concurrency"`
	}{Code: code, Manifest: manifest, Reasoning: opts.reasoning, Concurrency: opts.judgeConcurrency})
	if err != nil {
		return "", fmt.Errorf("marshal evaluator configuration: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(configuration)), nil
}

// writeManifest records which code and models produced the run.
func writeManifest(dir string, manifest runManifest) error {
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestFile), body, 0o600); err != nil {
		return fmt.Errorf("write run manifest: %w", err)
	}
	return nil
}

// loadManifest reads a run's manifest; a missing one is empty.
func loadManifest(dir string) (runManifest, error) {
	var manifest runManifest
	raw, err := os.ReadFile(filepath.Join(dir, manifestFile)) // #nosec G304 -- the run directory is a developer-chosen CLI path.
	if errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return manifest, fmt.Errorf("read run manifest: %w", err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, fmt.Errorf("decode run manifest: %w", err)
	}
	return manifest, nil
}
