package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	piopenrouter "github.com/speakeasy-api/gram/server/internal/scanners/promptinjection/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// caseStatus is a case's outcome in a run record.
type caseStatus string

const (
	// statusFlagged means the detector reported prompt injection.
	statusFlagged caseStatus = "flagged"

	// statusClear means the detector reached a verdict of no prompt injection.
	statusClear caseStatus = "clear"

	// statusNoVerdict means the detector reached no verdict, such as after a
	// refusal, a malformed response or a provider failure. It scores as a miss
	// for an attack.
	statusNoVerdict caseStatus = "no_verdict"

	// statusOutOfCredit means OpenRouter rejected a call for lack of credit.
	// The next run redoes the case.
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
)

// caseRecord is one case's result in a run, written to cases.jsonl as soon as
// the case finishes. Other versions of this tool read the same format, so a
// field's meaning must not change.
type caseRecord struct {
	// Key is the case's source and id, "<source>::<id>".
	Key string `json:"key"`

	// Hash identifies the case's fixture line. Editing a fixture changes it,
	// so the edited case runs again.
	Hash string `json:"hash"`

	// Status is the case outcome.
	Status caseStatus `json:"status"`

	// Model is the model whose verdict decided the case.
	Model string `json:"model,omitempty"`

	// Detail is the deciding model's rationale, or why no verdict was reached.
	Detail string `json:"detail,omitempty"`

	// Refused reports that the deciding model refused the case.
	Refused bool `json:"refused,omitempty"`

	// CostUSD is the provider-reported cost of the case's calls.
	CostUSD float64 `json:"cost_usd"`

	// LatencyMS is the case's total time in milliseconds.
	LatencyMS float64 `json:"latency_ms"`
}

// runManifest describes a run directory: which detector produced it.
type runManifest struct {
	// Label names the side of a comparison, such as "main".
	Label string `json:"label"`

	// Ref names the code, such as "origin/main @ 1a2b3c4d5e".
	Ref string `json:"ref"`

	// PrefilterModel and PrefilterThreshold describe a prefilter stage; the
	// single judge has none.
	PrefilterModel string `json:"prefilter_model"`

	// PrefilterThreshold is the prefilter probability that escalates a case.
	PrefilterThreshold float64 `json:"prefilter_threshold"`

	// ConfirmationModel is the model that decides each case.
	ConfirmationModel string `json:"confirmation_model"`

	// ConfirmationPromptSHA256 hashes the deciding model's system prompt.
	ConfirmationPromptSHA256 string `json:"confirmation_prompt_sha256"`

	// PrefilterQuestionsSHA256 hashes a prefilter's questions; empty without one.
	PrefilterQuestionsSHA256 string `json:"prefilter_questions_sha256"`

	// Updated is when the run last wrote its manifest.
	Updated time.Time `json:"updated"`
}

// caseOutcome is what the detector produced for one case.
type caseOutcome struct {
	findings    []scanners.Finding
	verdict     piopenrouter.Stabilized
	observation decisionObservation
}

// caseKey identifies a case across runs.
func caseKey(c labeledCase) string {
	return c.Source + "::" + c.ID
}

// caseHash fingerprints a case's fixture line, so every version of this tool
// gives the same case the same hash and an edited fixture runs again. A case
// built in code, without a fixture line, hashes its fields.
func caseHash(c labeledCase) string {
	raw := []byte(c.raw)
	if c.raw == "" {
		marshalled, err := json.Marshal(c)
		if err != nil {
			marshalled = []byte(caseKey(c))
		}
		raw = marshalled
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
	status, ok := errors.AsType[*openrouter.HTTPError](err)
	return ok && status.StatusCode == http.StatusPaymentRequired
}

// recordFromOutcome turns a finished case into its run record.
func recordFromOutcome(c labeledCase, model string, o caseOutcome) caseRecord {
	rec := caseRecord{
		Key:       caseKey(c),
		Hash:      caseHash(c),
		Status:    statusClear,
		Model:     model,
		Detail:    truncateRunes(o.verdict.Rationale, maxDetailRunes),
		Refused:   false,
		CostUSD:   0,
		LatencyMS: float64(o.observation.Latency) / float64(time.Millisecond),
	}
	var callErr error
	outOfCredit := false
	for _, call := range o.observation.Calls {
		rec.CostUSD += call.CostUSD
		if call.Err != nil {
			callErr = call.Err
			outOfCredit = outOfCredit || isOutOfCredit(call.Err)
		}
	}
	switch {
	case outOfCredit:
		rec.Status = statusOutOfCredit
		rec.Detail = "OpenRouter returned 402: out of credit"
	case callErr != nil:
		rec.Status = statusNoVerdict
		rec.Detail = truncateRunes(callErr.Error(), maxDetailRunes)
	case len(o.findings) > 0:
		rec.Status = statusFlagged
	}
	return rec
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// loadRecords reads a run's records by case key; a later line replaces an
// earlier one. A missing file is an empty run, and a malformed line, such as a
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

// casesToRun lists the cases without a usable record: new or edited cases,
// and cases a previous run could not fund.
func casesToRun(corpus []labeledCase, records map[string]caseRecord) []labeledCase {
	var todo []labeledCase
	for _, c := range corpus {
		rec, ok := records[caseKey(c)]
		if !ok || rec.Hash != caseHash(c) || rec.Status == statusOutOfCredit {
			todo = append(todo, c)
		}
	}
	return todo
}

// runTally counts a run's outcomes over the corpus for the summary line.
type runTally struct {
	cases, done, falsePositives, attacks, caught, noVerdict, outOfCredit int
	costUSD                                                              float64
}

func tallyRun(corpus []labeledCase, records map[string]caseRecord) runTally {
	var t runTally
	for _, c := range corpus {
		t.cases++
		malicious := c.Label == "malicious"
		if malicious {
			t.attacks++
		}
		rec, ok := records[caseKey(c)]
		if !ok || rec.Hash != caseHash(c) {
			continue
		}
		t.costUSD += rec.CostUSD
		switch rec.Status {
		case statusOutOfCredit:
			t.outOfCredit++
			continue
		case statusNoVerdict:
			t.noVerdict++
		case statusFlagged:
			if malicious {
				t.caught++
			} else {
				t.falsePositives++
			}
		case statusClear:
		}
		t.done++
	}
	return t
}

func (t runTally) line(label string) string {
	return fmt.Sprintf("%s: %d/%d cases · FP %d · caught %d/%d · no verdict %d · out of credit %d · $%.2f",
		label, t.done, t.cases, t.falsePositives, t.caught, t.attacks, t.noVerdict, t.outOfCredit, t.costUSD)
}

// runRecords evaluates the production detector on the cases a run directory
// lacks, writing each record as it finishes. A run that runs out of credit
// stops at once and keeps its records, so the next run continues from there.
func runRecords(ctx context.Context, opts options, corpus []labeledCase) error {
	if err := os.MkdirAll(opts.runDir, 0o750); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}
	casesPath := filepath.Join(opts.runDir, casesFile)
	records, err := loadRecords(casesPath)
	if err != nil {
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
		stopped, err = evaluateIntoRun(ctx, opts, key, corpus, todo, records, casesPath)
		if err != nil {
			return err
		}
	}
	if err := writeManifest(opts); err != nil {
		return err
	}
	tally := tallyRun(corpus, records)
	fmt.Fprintln(os.Stderr, tally.line(opts.label))
	if stopped || tally.outOfCredit > 0 {
		return fmt.Errorf("%s: out of OpenRouter credit with %d cases left; add credit at https://openrouter.ai/settings/credits and rerun to continue", opts.label, tally.cases-tally.done)
	}
	return nil
}

// evaluateIntoRun judges todo, appending each finished case to the run. It
// reports whether the run stopped for lack of credit.
func evaluateIntoRun(ctx context.Context, opts options, key string, corpus, todo []labeledCase, records map[string]caseRecord, casesPath string) (bool, error) {
	file, err := os.OpenFile(casesPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- the run directory is a developer-chosen CLI path.
	if err != nil {
		return false, fmt.Errorf("open run records for append: %w", err)
	}
	defer o11y.NoLogDefer(file.Close)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu      sync.Mutex
		stopped bool
		done    int
		addErr  error
	)
	onCase := func(i int, o caseOutcome) {
		rec := recordFromOutcome(todo[i], opts.judgeModel, o)
		mu.Lock()
		defer mu.Unlock()
		// After a stop, calls cut short by the cancellation leave the case for
		// the next run rather than recording a failure it did not have.
		if stopped && rec.Status == statusNoVerdict {
			return
		}
		if rec.Status == statusOutOfCredit && !stopped {
			stopped = true
			cancel()
		}
		line, err := json.Marshal(rec)
		if err != nil {
			addErr = errors.Join(addErr, fmt.Errorf("marshal run record: %w", err))
			return
		}
		if _, err := file.Write(append(line, '\n')); err != nil {
			addErr = errors.Join(addErr, fmt.Errorf("append run record: %w", err))
			return
		}
		records[rec.Key] = rec
		done++
		if done%progressEvery == 0 {
			fmt.Fprintln(os.Stderr, tallyRun(corpus, records).line(opts.label))
		}
	}
	if _, _, err := scanJudge(runCtx, opts, newOpenRouterClient(key), todo, onCase); err != nil {
		return false, err
	}
	return stopped, addErr
}

// writeManifest records which detector produced the run.
func writeManifest(opts options) error {
	promptHash := sha256.Sum256([]byte(piopenrouter.SystemPrompt))
	manifest := runManifest{
		Label:                    opts.label,
		Ref:                      opts.ref,
		PrefilterModel:           "",
		PrefilterThreshold:       0,
		ConfirmationModel:        opts.judgeModel,
		ConfirmationPromptSHA256: fmt.Sprintf("%x", promptHash),
		PrefilterQuestionsSHA256: "",
		Updated:                  time.Now().UTC(),
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(opts.runDir, manifestFile), body, 0o600); err != nil {
		return fmt.Errorf("write run manifest: %w", err)
	}
	return nil
}
