package risk_analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// PIIScanner detects personally identifiable information in text.
type PIIScanner interface {
	// AnalyzeBatch sends multiple texts to the PII analyzer and returns
	// findings for each. The outer slice is indexed by input position.
	AnalyzeBatch(ctx context.Context, texts []string) ([][]Finding, error)
}

// presidioRequest is the payload sent to POST /analyze.
type presidioRequest struct {
	Text     string  `json:"text"`
	Language string  `json:"language"`
	ScoreMin float64 `json:"score_threshold"`
}

// presidioResult is a single entity returned by the analyzer.
type presidioResult struct {
	EntityType    string  `json:"entity_type"`
	Start         int     `json:"start"`
	End           int     `json:"end"`
	Score         float64 `json:"score"`
	RecognizerKey string  `json:"recognition_metadata,omitempty"`
}

// PresidioClient calls the Presidio Analyzer HTTP API.
type PresidioClient struct {
	baseURL    string
	httpClient *http.Client //nolint:forbidigo // Injected via guardian.Policy in the wiring layer.
}

// NewPresidioClient creates a client pointing at the given base URL.
// The httpClient should be obtained from guardian.Policy.PooledClient().
func NewPresidioClient(baseURL string, httpClient *http.Client) *PresidioClient { //nolint:forbidigo // Accepts guardian-provided client.
	return &PresidioClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (p *PresidioClient) AnalyzeBatch(ctx context.Context, texts []string) ([][]Finding, error) {
	n := len(texts)
	if n == 0 {
		return nil, nil
	}

	results := make([][]Finding, n)
	workers := min(runtime.NumCPU(), n)

	ch := make(chan int, n)
	for i := range n {
		ch <- i
	}
	close(ch)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)

	for range workers {
		wg.Go(func() {
			for idx := range ch {
				mu.Lock()
				failed := firstErr != nil
				mu.Unlock()
				if failed {
					return
				}

				findings, err := p.analyze(ctx, texts[idx])
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("presidio analyze text %d: %w", idx, err)
					}
					mu.Unlock()
					return
				}
				results[idx] = findings
			}
		})
	}

	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

func (p *PresidioClient) analyze(ctx context.Context, text string) ([]Finding, error) {
	body, err := json.Marshal(presidioRequest{
		Text:     text,
		Language: "en",
		ScoreMin: 0.5,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal presidio request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create presidio request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("presidio http request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("presidio returned status %d", resp.StatusCode)
	}

	var results []presidioResult
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode presidio response: %w", err)
	}

	// Presidio returns character (rune) offsets, not byte offsets.
	// Convert to runes for correct slicing, then map back to byte positions.
	runes := []rune(text)

	findings := make([]Finding, 0, len(results))
	for _, r := range results {
		match := ""
		if r.Start >= 0 && r.End >= r.Start && r.End <= len(runes) {
			match = string(runes[r.Start:r.End])
		}

		// Convert rune offsets to byte offsets for storage.
		startByte := len(string(runes[:r.Start]))
		endByte := len(string(runes[:r.End]))

		findings = append(findings, Finding{
			RuleID:      r.EntityType,
			Description: "PII detected: " + r.EntityType,
			Match:       match,
			StartPos:    startByte,
			EndPos:      endByte,
			Tags:        []string{"pii", strings.ToLower(r.EntityType)},
			Source:      "presidio",
			Confidence:  r.Score,
		})
	}
	return findings, nil
}

// StubPIIScanner is a no-op implementation for environments without Presidio.
type StubPIIScanner struct{}

func (s *StubPIIScanner) AnalyzeBatch(_ context.Context, texts []string) ([][]Finding, error) {
	results := make([][]Finding, len(texts))
	for i := range texts {
		results[i] = nil
	}
	return results, nil
}
