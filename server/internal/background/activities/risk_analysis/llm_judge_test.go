package risk_analysis

import "testing"

func TestParseJudgeConfigTrimsWhitespaceModel(t *testing.T) {
	t.Parallel()

	cfg := ParseJudgeConfig([]byte(`{"model":"   ","fail_open":false}`))

	if cfg.Model != "" {
		t.Fatalf("Model = %q, want empty default model", cfg.Model)
	}
	if cfg.FailOpen {
		t.Fatalf("FailOpen = true, want false")
	}
}
