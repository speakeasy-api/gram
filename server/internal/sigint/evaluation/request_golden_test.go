package evaluation

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestSingleMessageModelRequestGolden(t *testing.T) {
	t.Parallel()
	assertConversationModelRequest(t, InputStrategySingleMessage)
}

func TestSlidingWindowModelRequestGolden(t *testing.T) {
	t.Parallel()
	assertConversationModelRequest(t, InputStrategySlidingWindow)
}

func assertConversationModelRequest(t *testing.T, strategy string) {
	t.Helper()
	m := message()
	m.SetRole(conversationv1.MessageEvent_ROLE_ASSISTANT)
	definitions := sensors()
	for i := range definitions {
		definitions[i].InputStrategy = strategy
		definitions[i].WindowSize = 5
		definitions[i].MatchExpression = `message.role == "assistant"`
	}
	definitions[0].Instructions = new("Identify the services mentioned by the target.")
	definitions[0].Signals = []classifier.Option{classifier.NewOption("a", classifier.Text("Billing")), classifier.NewOption("b", classifier.Text("Authentication"))}
	definitions[1].Instructions = new("Choose the main service discussed by the target.")
	definitions[1].Signals = definitions[0].Signals
	definitions[2].Instructions = new("Rate the target's customer impact from low to high.")
	definitions[2].Signals = []classifier.Option{classifier.NewOption("a", classifier.Text("Low impact")), classifier.NewOption("b", classifier.Text("High impact"))}

	target := storedMessage(m)
	target.Content = "Searchable fallback must not duplicate the original payload."
	contentURI := "gs://test-bucket/" + m.GetProjectId() + "/message.json"
	attachmentURI := "gs://test-bucket/" + m.GetProjectId() + "/attachment.txt"
	target.ContentAssetUrl = conv.ToPGText(contentURI)
	target.ToolCalls = []byte(`[{"id":"call-check","function":{"name":"check_payment","arguments":"{\"invoice\":\"example-invoice\"}"}}]`)
	target.AttachmentUris = []string{attachmentURI}
	blobs := memoryBlobs{
		contentURI:    []byte(`{"text":"I will check whether the payment retry succeeded.","reference":9007199254740993}`),
		attachmentURI: []byte("Payment attempts are failing for the entire team."),
	}

	// HistorySource supplies non-system history newest first. The oldest row is
	// deliberately outside the five-message window. Other context roles need not
	// match the target's sensor predicate.
	var history []repo.LoadEvaluationHistoryRow
	for _, item := range []struct{ role, content string }{
		{"tool", "Payment declined: retry required."},
		{"assistant", "Checking the invoice."},
		{"user", "Our billing payments are failing. Sign-in still works."},
		{"user", "The first failed charge was this morning."},
		{"user", "This older message is outside the window."},
	} {
		row := targetContent(storedMessage(m))
		row.ID = uuid.New()
		row.Role, row.Content = item.role, item.content
		history = append(history, row)
	}
	// Split transcript rows must expose row-local text and calls, not the archive.
	history[1].RowLocalContent = true
	history[1].ContentRaw = []byte(`[{"text":"Archived sibling content must not appear."}]`)
	// Incomplete streamed arguments remain evidence rather than being reparsed.
	history[1].ToolCalls = []byte(`[{"id":"call-invoice","function":{"name":"lookup_invoice","arguments":"{\"invoice\":"}}]`)
	c, requests := newRecordingClassifier(t)
	h, pub := handler(t, m, definitions, c, testenv.NewMeterProvider(t))
	h.messages = &windowMessages{storedMessages: storedMessages{target.ID: target}, history: history}
	h.blobs = blobs
	require.NoError(t, h.Handle(t.Context(), m, gcp.MessageMetadata{}))
	require.Len(t, pub.readings, 3)
	testenv.RequireYAMLGolden(t, "testdata/requests/"+strategy+".golden.yaml", requests.snapshot(t))
}
