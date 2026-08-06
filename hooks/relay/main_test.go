package relay

import (
	"os"
	"testing"
)

// TestMain disables the telemetry recorder for the whole package: every
// runner a test builds would otherwise stand up a real OTel batch exporter
// pointed at the test's fake ingest server, whose scheduled export races the
// test's request-count assertions. Individual telemetry tests opt back in by
// clearing the variable with t.Setenv.
func TestMain(m *testing.M) {
	if err := os.Setenv("GRAM_HOOKS_DISABLE_TELEMETRY", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
