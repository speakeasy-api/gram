package glint

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestNoDirectChatMessageInsert(t *testing.T) {
	t.Parallel()

	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, newNoDirectChatMessageInsertAnalyzer(noDirectChatMessageInsertSettings{}), "github.com/speakeasy-api/gram/server/internal/hooks")
}
