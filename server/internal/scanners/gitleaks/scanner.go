// Package gitleaks is the single home for gitleaks-based secret scanning. It
// owns the gitleaks detector lifecycle, batch/single scanning, and conversion
// of raw gitleaks findings into the shared scanners.Finding domain type. Both
// the streams subscriber (this package's Handler) and the risk_analysis
// activity consume it.
//
// The detector and the raw gitleaks finding type are deliberately kept
// unexported: callers scan strings and receive scanners.Finding values, never
// touching *detect.Detector or report.Finding.
package gitleaks

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

// Source labels findings produced by this scanner. The shared Finding topic
// distinguishes detection sources by this value.
const Source = "gitleaks"

// prefixSecret is prepended to canonical gitleaks rule ids.
const prefixSecret = "secret."

// detectorInitMu serializes gitleaks detector creation process-wide. Gitleaks
// uses viper internally which has global state that races on concurrent
// initialization.
var detectorInitMu sync.Mutex

// newDetector creates a gitleaks detector using the default config extended with
// our AWS credential rules (see awsRules), serialized by detectorInitMu to avoid
// viper's init-time data race.
//
// The AWS rules — in particular the composite aws-secret-access-key-paired rule
// — carry keywords that must be present in the detector's aho-corasick prefilter,
// which NewDetector builds once from Config.Keywords. So we inject the rules and
// their keywords into the default config and construct the detector from the
// extended config, rather than mutating a detector after the fact (which would
// leave the prefilter stale and silently skip keyworded rules).
func newDetector() (*detect.Detector, error) {
	detectorInitMu.Lock()
	defer detectorInitMu.Unlock()
	base, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}

	cfg := base.Config
	for _, rule := range awsRules() {
		// Validate explicitly: constructing config.Rule values in Go bypasses the
		// TOML translation path that would normally call Validate, so a malformed
		// rule (e.g. a SecretGroup past the regex's capture count) would otherwise
		// fail silently as a rule that never matches. Surface it at startup.
		if err := rule.Validate(); err != nil {
			return nil, fmt.Errorf("invalid AWS gitleaks rule %q: %w", rule.RuleID, err)
		}
		cfg.Rules[rule.RuleID] = rule
		cfg.OrderedRules = append(cfg.OrderedRules, rule.RuleID)
		for _, k := range rule.Keywords {
			cfg.Keywords[strings.ToLower(k)] = struct{}{}
		}
	}

	return detect.NewDetector(cfg), nil
}

// Scanner is the single gitleaks scanner used across the codebase — batch
// fan-out (the risk-analysis activity), the streams subscriber, and the
// real-time enforcement path. It holds a fixed, warm set of at most NumCPU
// detectors on a buffered channel that doubles as a semaphore. Concurrent Scan
// calls check a detector out (exclusive ownership — DetectString is not safe for
// concurrent use on a single instance) and return it warm, so each detector's
// expensive rule compilation is paid once and reused for the Scanner's lifetime.
// Detectors are created lazily on first use of each slot, so a serial caller
// only ever materializes one. A long-lived caller that wants the first scan
// warm can prime the Scanner once at startup with a throwaway Scan.
type Scanner struct {
	// detectors is a fixed-capacity free list. A nil slot is one whose detector
	// has not been created yet; a non-nil slot is a warm, reusable detector.
	// Its capacity caps both the live detector count and Scan concurrency.
	detectors chan *detect.Detector
}

// NewScanner creates a new Scanner with a warm detector set sized to NumCPU.
func NewScanner() *Scanner {
	n := runtime.NumCPU()
	s := &Scanner{detectors: make(chan *detect.Detector, n)}
	for range n {
		s.detectors <- nil
	}
	return s
}

// Prime eagerly materializes one detector and returns it to the warm set so
// the first Scan does not pay rule compilation on the hot path, and any
// detector-init failure surfaces at startup rather than on the first request.
// It is intended to be called once, during construction, before the Scanner is
// shared across goroutines. It is not idempotent: because the free list is a
// FIFO of initially-nil slots, each call materializes one more detector (up to
// NumCPU) rather than reusing the first — repeated calls just warm extra slots,
// which is harmless but not a no-op.
func (s *Scanner) Prime() error {
	select {
	case d := <-s.detectors:
		if d != nil {
			// Already warm; hand it back untouched.
			s.detectors <- d
			return nil
		}
		nd, err := newDetector()
		if err != nil {
			s.detectors <- nil // leave the slot uncreated for a later retry
			return err
		}
		s.detectors <- nd
		return nil
	default:
		// Every slot is checked out (not possible during single-threaded
		// construction); nothing to prime.
		return nil
	}
}

// Scan scans a single message with a detector checked out from the warm set,
// blocking until a slot is free. Safe for concurrent use: each call holds its
// detector exclusively until it returns it.
func (s *Scanner) Scan(ctx context.Context, content string) ([]scanners.Finding, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("abort: %w", ctx.Err())
	default:
		// carry on
	}

	// Check a detector out of the warm set. This blocks when every slot is
	// checked out (concurrency saturated), so the receive must be context-aware:
	// otherwise a canceled request/workflow would stay parked here until an
	// unrelated Scan happens to return a detector. Only the receive case removes
	// from the channel, so a ctx.Done() abort leaves the semaphore untouched.
	var d *detect.Detector
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("abort: %w", ctx.Err())
	case d = <-s.detectors:
	}
	if d == nil {
		var err error
		d, err = newDetector()
		if err != nil {
			s.detectors <- nil // leave the slot uncreated for a later retry
			return nil, err
		}
	}
	defer func() { s.detectors <- d }()

	return convertFindings(content, d.DetectString(content)), nil
}

func (s *Scanner) ScanBatch(ctx context.Context, contents []string) ([][]scanners.Finding, error) {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())

	findings := make([][]scanners.Finding, len(contents))
	for i, content := range contents {
		g.Go(func() error {
			f, err := s.Scan(ctx, content)
			if err != nil {
				return fmt.Errorf("scan content: %w", err)
			}
			findings[i] = f
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return [][]scanners.Finding{}, fmt.Errorf("errgroup: %w", err)
	}

	return findings, nil
}

// convertFindings converts raw gitleaks findings to domain Findings. Rule ids
// are canonicalized to the shared snake_case-with-dots form and line/column
// positions are mapped to byte offsets. Gitleaks ships human-readable
// descriptions that never echo the matched secret, so they pass through.
func convertFindings(content string, raw []report.Finding) []scanners.Finding {
	out := make([]scanners.Finding, 0, len(raw))
	for _, f := range raw {
		startPos := lineColToBytePos(content, f.StartLine, f.StartColumn)
		endPos := min(lineColToBytePos(content, f.EndLine, f.EndColumn)+1, len(content))
		out = append(out, scanners.Finding{
			RuleID:      guardRuleID(CanonicalRuleID(f.RuleID)),
			Description: f.Description,
			Match:       f.Match,
			StartPos:    startPos,
			EndPos:      endPos,
			Tags:        parseTags(f.Tags),
			Source:      Source,
			Confidence:  1.0, // gitleaks is rule-based, always 1.0

			DeadLetterReason:    "",
			McpLookupToolCallID: "",
			SpanGroupKey:        "",
			Field:               "",
			Path:                "",
		})
	}
	return out
}

// guardRuleID panics in test builds when id is not canonical, catching rule-id
// drift early. CanonicalRuleID is the sole producer of gitleaks rule ids, so
// this only fires on a genuinely malformed upstream gitleaks rule name.
func guardRuleID(id string) string {
	if testing.Testing() {
		if err := scanners.ValidateRuleID(id); err != nil {
			panic(fmt.Sprintf("gitleaks: %v", err))
		}
	}
	return id
}

// CanonicalRuleID prepends the `secret.` prefix to a gitleaks rule id and
// converts its kebab-case to snake_case so the result conforms to the
// canonical rule-id grammar shared across scanners.
func CanonicalRuleID(raw string) string {
	return prefixSecret + strings.ReplaceAll(strings.ToLower(raw), "-", "_")
}

// lineColToBytePos converts line and column numbers to a byte position in
// content. We iterate by byte (not rune) to match gitleaks' accounting.
//
// Gitleaks computes a column as (bytePos - prevNewlineByteIndex) + 1, where
// prevNewlineByteIndex is the byte index of the '\n' that precedes the line
// (initialized to 0 for line 0) — see
// github.com/zricethezav/gitleaks/v8/detect/location.go. That formula has an
// asymmetry we must mirror: on line 0 the first byte is column 1, but on every
// later line the '\n' byte itself occupies the "column 1" slot, so the first
// byte *after* a newline is column 2. Resetting to column 1 after a newline
// would therefore shift every finding past the first line by one byte (dropping
// the first byte of the secret and picking up a stray trailing byte).
func lineColToBytePos(content string, line, col int) int {
	if line < 0 || col <= 0 {
		return 0
	}

	currentLine := 0
	currentCol := 1

	for i := 0; i < len(content); i++ {
		if currentLine == line && currentCol == col {
			return i
		}

		if content[i] == '\n' {
			currentLine++
			// The byte right after the newline is column 2 in gitleaks'
			// accounting (the newline holds column 1), not column 1.
			currentCol = 2
		} else {
			currentCol++
		}
	}

	return len(content)
}

func parseTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	var out []string
	for _, t := range tags {
		for part := range strings.SplitSeq(t, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
