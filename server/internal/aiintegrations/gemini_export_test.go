package aiintegrations

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Synthetic fixtures exercise the documented fields with the provisional
// element layout described in gemini_export.go, not a captured Vault export.
const syntheticGeminiTurn = `<ConversationTurn><UserPrompt>repeat &amp; explain &lt;x&gt;</UserPrompt><GeminiAppResponse><![CDATA[Text <answer>]]></GeminiAppResponse><GeminiModelVersion>synthetic-model</GeminiModelVersion><Parent-request_ID>parent-1</Parent-request_ID><Timestamp>2026-01-02T03:04:05+0000</Timestamp></ConversationTurn>`
const syntheticGeminiReport = `<ErrorReport><Summary><AccountErrorsCount>0</AccountErrorsCount><PartialAccountErrorsCount>0</PartialAccountErrorsCount></Summary><AccountErrors/><PartialAccountErrors/></ErrorReport>`

func syntheticGeminiDocument(owner, conversation, turns string) string {
	return `<Export><Email>` + owner + `</Email><Conversations><Conversation><ConversationId>` + conversation + `</ConversationId><ConversationTopic>Synthetic &amp; example</ConversationTopic>` + turns + `</Conversation></Conversations></Export>`
}

func syntheticGeminiZIP(t *testing.T, files ...geminiExportFile) geminiExportFile {
	t.Helper()
	// Synthetic exports include their mandatory completeness sidecars.
	counts := map[string]int{}
	ids := map[string]bool{}
	hasCounts, hasReport := false, false
	for _, file := range files {
		if strings.HasSuffix(file.Name, "-result-counts.csv") {
			hasCounts = true
			continue
		}
		if strings.HasSuffix(file.Name, "-error.xml") {
			hasReport = true
			continue
		}
		if strings.HasSuffix(file.Name, ".xml") {
			turns, _ := parseGeminiConversationXML(context.Background(), file.Content)
			for _, turn := range turns {
				if !ids[turn.ID] {
					counts[turn.OwnerEmail]++
					ids[turn.ID] = true
				}
			}
		}
	}
	if !hasCounts {
		summary := "Email,AccountStatus,SuccessCount\n"
		owners := make([]string, 0, len(counts))
		for owner := range counts {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
		for _, owner := range owners {
			summary += fmt.Sprintf("%s,Success,%d\n", owner, counts[owner])
		}
		files = append(files, geminiExportFile{Name: "export-result-counts.csv", Content: []byte(summary)})
	}
	if !hasReport {
		files = append(files, geminiExportFile{Name: "export-error.xml", Content: []byte(syntheticGeminiReport)})
	}
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, file := range files {
		w, err := writer.Create(file.Name)
		require.NoError(t, err)
		_, err = w.Write(file.Content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return geminiExportFile{Name: "synthetic.zip", Content: buf.Bytes()}
}

func collectGeminiExport(t *testing.T, files ...geminiExportFile) ([]geminiTurn, error) {
	t.Helper()
	var turns []geminiTurn
	err := parseGeminiExport(context.Background(), files, func(turn geminiTurn) error { turns = append(turns, turn); return nil })
	return turns, err
}

func TestGeminiExportOwnersConversationsAndMetadata(t *testing.T) {
	t.Parallel()
	secondTurn := strings.ReplaceAll(syntheticGeminiTurn, "03:04:05", "03:04:06")
	first := syntheticGeminiDocument("FIRST@example.invalid", "shared-id", syntheticGeminiTurn+secondTurn)
	// Namespace prefixes and default namespaces must not affect field matching.
	second := `<v:Export xmlns:v="urn:synthetic-vault"><v:Email>second@example.invalid</v:Email><v:Conversation><v:ConversationId>shared-id</v:ConversationId>` + syntheticGeminiTurn + `</v:Conversation><v:Conversation><v:ConversationId>other-id</v:ConversationId>` + syntheticGeminiTurn + `</v:Conversation></v:Export>`
	archive := syntheticGeminiZIP(t,
		geminiExportFile{Name: "one.xml", Content: []byte(first)},
		geminiExportFile{Name: "two.xml", Content: []byte(second)},
		geminiExportFile{Name: "export-result-counts.csv", Content: []byte("Email,AccountStatus,SuccessCount\r\nfirst@example.invalid,Success,2\r\nsecond@example.invalid,Success,2\r\n")},
	)
	report := geminiExportFile{Name: "export-error.xml", Content: []byte(syntheticGeminiReport)}
	turns, err := collectGeminiExport(t, archive, report)
	require.NoError(t, err)
	require.Len(t, turns, 4)
	require.Equal(t, "first@example.invalid", turns[0].OwnerEmail)
	require.Equal(t, "Synthetic & example", turns[0].Title)
	require.Equal(t, "repeat & explain <x>", turns[0].Prompt)
	require.Equal(t, "Text <answer>", turns[0].Response)
	require.Equal(t, "synthetic-model", turns[0].Model)
	require.Equal(t, "parent-1", turns[0].ParentRequestID)
	require.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), turns[0].Timestamp)
	ids := make(map[string]bool)
	for _, turn := range turns {
		require.NotEmpty(t, turn.ID)
		require.False(t, ids[turn.ID])
		ids[turn.ID] = true
	}
	repeated, err := collectGeminiExport(t, archive, archive, report)
	require.NoError(t, err)
	require.Equal(t, turns, repeated)
}

func TestGeminiExportStableIdentityAndVariants(t *testing.T) {
	t.Parallel()
	document := syntheticGeminiDocument("user@example.invalid", "conversation", syntheticGeminiTurn)
	first, err := collectGeminiExport(t, syntheticGeminiZIP(t, geminiExportFile{Name: "first.xml", Content: []byte(document)}))
	require.NoError(t, err)
	variants := strings.NewReplacer("UserPrompt", "Prompt", "GeminiAppResponse", "Response", "GeminiModelVersion", "Model", "+0000", "Z").Replace(document)
	second, err := collectGeminiExport(t, syntheticGeminiZIP(t, geminiExportFile{Name: "renamed.xml", Content: []byte(variants)}))
	require.NoError(t, err)
	require.Equal(t, first, second)
	// Distinct request IDs disambiguate identical text/timestamps.
	withIDs := strings.Replace(syntheticGeminiTurn, "<Timestamp>", "<RequestId>request-1</RequestId><Timestamp>", 1)
	otherID := strings.Replace(withIDs, "request-1", "request-2", 1)
	result, err := collectGeminiExport(t, syntheticGeminiZIP(t, geminiExportFile{Name: "requests.xml", Content: []byte(syntheticGeminiDocument("user@example.invalid", "conversation", withIDs+otherID))}))
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.NotEqual(t, result[0].ID, result[1].ID)
	require.Equal(t, "request-1", result[0].RequestID)
	// Request IDs with conflicting bodies must fail rather than drop a revision.
	conflict := strings.Replace(otherID, "request-2", "request-1", 1)
	conflict = strings.Replace(conflict, "Text <answer>", "Changed", 1)
	result, err = collectGeminiExport(t, syntheticGeminiZIP(t, geminiExportFile{Name: "conflict.xml", Content: []byte(syntheticGeminiDocument("user@example.invalid", "conversation", withIDs+conflict))}))
	require.Error(t, err)
	require.Empty(t, result)
}

func TestGeminiExportRejectsInvalidContent(t *testing.T) {
	t.Parallel()
	valid := syntheticGeminiDocument("user@example.invalid", "conversation", syntheticGeminiTurn)
	cases := map[string]string{
		"empty":                "",
		"unknown schema":       `<Export><Message>unrecognized</Message></Export>`,
		"no turns":             syntheticGeminiDocument("user@example.invalid", "conversation", ""),
		"invalid timestamp":    strings.Replace(valid, "2026-01-02T03:04:05+0000", "yesterday", 1),
		"missing timestamp":    strings.Replace(valid, `<Timestamp>2026-01-02T03:04:05+0000</Timestamp>`, "", 1),
		"missing owner":        strings.Replace(valid, "user@example.invalid", "", 1),
		"missing conversation": strings.Replace(valid, "<ConversationId>conversation</ConversationId>", "", 1),
		"unknown prompt":       strings.ReplaceAll(valid, "UserPrompt", "UnrecognizedPrompt"),
		"missing response":     strings.ReplaceAll(valid, "GeminiAppResponse", "UnrecognizedResponse"),
		"nested content":       strings.Replace(valid, "repeat &amp; explain &lt;x&gt;", "<Text>body</Text>", 1),
		"attachment":           strings.Replace(valid, "</ConversationTurn>", "<Attachments><Attachment>image</Attachment></Attachments></ConversationTurn>", 1),
		"duplicate field":      strings.Replace(valid, "</ConversationTurn>", "<Prompt>other</Prompt></ConversationTurn>", 1),
		"malformed":            valid + "<",
		"doctype":              `<!DOCTYPE x [<!ENTITY data SYSTEM "file:///nonexistent">]>` + valid,
		"depth limit":          strings.Repeat("<Wrapper>", geminiMaxXMLDepth) + valid + strings.Repeat("</Wrapper>", geminiMaxXMLDepth),
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := collectGeminiExport(t, syntheticGeminiZIP(t, geminiExportFile{Name: "content.xml", Content: []byte(document)}))
			require.Error(t, err)
			require.Empty(t, result)
		})
	}
}

func TestGeminiExportRejectsPartialFailures(t *testing.T) {
	t.Parallel()
	content := syntheticGeminiZIP(t, geminiExportFile{Name: "content.xml", Content: []byte(syntheticGeminiDocument("user@example.invalid", "conversation", syntheticGeminiTurn))})
	cases := map[string]string{
		"account":         strings.Replace(syntheticGeminiReport, "<AccountErrorsCount>0", "<AccountErrorsCount>1", 1),
		"partial account": strings.Replace(syntheticGeminiReport, "<PartialAccountErrorsCount>0", "<PartialAccountErrorsCount>1", 1),
		"account list":    strings.Replace(syntheticGeminiReport, "<AccountErrors/>", "<AccountErrors><Account>user@example.invalid</Account></AccountErrors>", 1),
		"conversation":    strings.Replace(syntheticGeminiReport, "</ErrorReport>", "<ConversationErrors><Conversation>failed</Conversation></ConversationErrors></ErrorReport>", 1),
		"bad count":       strings.Replace(syntheticGeminiReport, "<AccountErrorsCount>0", "<AccountErrorsCount>unknown", 1),
		"unknown":         `<Report/>`,
	}
	for name, report := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := collectGeminiExport(t, content, geminiExportFile{Name: "export-error.xml", Content: []byte(report)})
			require.Error(t, err)
			require.Equal(t, name != "bad count" && name != "unknown", errors.Is(err, errGeminiPartialExport))
			require.Empty(t, result)
		})
	}
}

func TestGeminiExportResultCounts(t *testing.T) {
	t.Parallel()
	content := syntheticGeminiZIP(t, geminiExportFile{Name: "content.xml", Content: []byte(syntheticGeminiDocument("user@example.invalid", "conversation", syntheticGeminiTurn))})
	cases := map[string]string{
		"partial":         "Email,AccountStatus,SuccessCount\nuser@example.invalid,PartialAccountError,1\n",
		"failed":          "Email,AccountStatus,SuccessCount\nuser@example.invalid,AccountError,0\n",
		"mismatch":        "Email,AccountStatus,SuccessCount\nuser@example.invalid,Success,2\n",
		"missing owner":   "Email,AccountStatus,SuccessCount\nother@example.invalid,Success,0\n",
		"negative":        "Email,AccountStatus,SuccessCount\nuser@example.invalid,Success,-1\n",
		"malformed":       "Email,AccountStatus,SuccessCount\nuser@example.invalid,Success\n",
		"empty":           "Email,AccountStatus,SuccessCount\n",
		"unsupported":     "Email,Count\nuser@example.invalid,1\n",
		"duplicate owner": "Email,AccountStatus,SuccessCount\nuser@example.invalid,Success,1\nuser@example.invalid,Success,1\n",
	}
	for name, summary := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			turns, err := collectGeminiExport(t, content, geminiExportFile{Name: "export-result-counts.csv", Content: []byte(summary)})
			require.Error(t, err)
			require.Equal(t, name == "partial" || name == "failed", errors.Is(err, errGeminiPartialExport))
			require.Empty(t, turns)
		})
	}
	turns, err := collectGeminiExport(t, content, geminiExportFile{Name: "export-result-counts.csv", Content: []byte("\xef\xbb\xbfEmail,AccountStatus,SuccessCount\nuser@example.invalid,Success,1\nzero@example.invalid,Success,0\n")})
	require.NoError(t, err)
	require.Len(t, turns, 1)
}

func TestGeminiExportArchiveLimitsAndCancellation(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		file geminiExportFile
		// tooLarge errors make the sync split the export window.
		tooLarge bool
	}{
		"not zip":      {file: geminiExportFile{Name: "bad.zip", Content: []byte("bad")}},
		"traversal":    {file: syntheticGeminiZIP(t, geminiExportFile{Name: "../content.xml", Content: []byte("<Export/>")})},
		"unknown file": {file: syntheticGeminiZIP(t, geminiExportFile{Name: "content.json", Content: []byte(`{}`)})},
		"oversize":     {file: syntheticGeminiZIP(t, geminiExportFile{Name: "content.xml", Content: bytes.Repeat([]byte("x"), geminiMaxFileBytes+1)}), tooLarge: true},
		"ratio":        {file: syntheticGeminiZIP(t, geminiExportFile{Name: "content.xml", Content: bytes.Repeat([]byte("x"), 2<<20)})},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := collectGeminiExport(t, tc.file)
			require.Error(t, err)
			require.Equal(t, tc.tooLarge, errors.Is(err, errGeminiExportTooLarge))
		})
	}
	tooMany := make([]geminiExportFile, geminiMaxFiles+1)
	require.ErrorIs(t, parseGeminiExport(context.Background(), tooMany, func(geminiTurn) error { return nil }), errGeminiExportTooLarge)
	valid := syntheticGeminiZIP(t, geminiExportFile{Name: "content.xml", Content: []byte(syntheticGeminiDocument("user@example.invalid", "conversation", syntheticGeminiTurn))})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := parseGeminiExport(ctx, []geminiExportFile{valid}, func(geminiTurn) error { t.Fatal("visitor called after cancellation"); return nil })
	require.ErrorIs(t, err, context.Canceled)
	sentinel := errors.New("stop")
	err = parseGeminiExport(context.Background(), []geminiExportFile{valid}, func(geminiTurn) error { return sentinel })
	require.ErrorIs(t, err, sentinel)
	require.NotErrorIs(t, err, errGeminiPartialExport)
	require.Error(t, parseGeminiExport(context.Background(), nil, func(geminiTurn) error { return nil }))
	require.Error(t, parseGeminiExport(context.Background(), []geminiExportFile{valid}, nil))
}

func TestGeminiExportEmptyWindowRequiresCompletenessProof(t *testing.T) {
	t.Parallel()
	summary := geminiExportFile{Name: "export-result-counts.csv", Content: []byte("Email,AccountStatus,SuccessCount\nuser@example.invalid,Success,0\n")}
	report := geminiExportFile{Name: "export-error.xml", Content: []byte(syntheticGeminiReport)}
	// Vault can return just sidecars for a daily window with no matching turns.
	turns, err := collectGeminiExport(t, summary, report)
	require.NoError(t, err)
	require.Empty(t, turns)
	emptyOwner := geminiExportFile{Name: "owner.xml", Content: []byte(`<Export><Email>user@example.invalid</Email></Export>`)}
	turns, err = collectGeminiExport(t, summary, report, emptyOwner)
	require.NoError(t, err)
	require.Empty(t, turns)
	for name, files := range map[string][]geminiExportFile{
		"missing summary":                   {report},
		"missing report":                    {summary},
		"no files":                          nil,
		"nonzero":                           {{Name: summary.Name, Content: []byte("Email,AccountStatus,SuccessCount\nuser@example.invalid,Success,1\n")}, report},
		"unknown schema despite zero proof": {summary, report, {Name: "owner.xml", Content: []byte(`<Export><Unknown>nonempty unrecognized record</Unknown></Export>`)}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := collectGeminiExport(t, files...)
			require.Error(t, err)
			require.Empty(t, result)
		})
	}
}

func TestGeminiExportAdditionalMetadataAndEnclosingTimestamp(t *testing.T) {
	t.Parallel()
	turn := strings.Replace(syntheticGeminiTurn, `<Timestamp>2026-01-02T03:04:05+0000</Timestamp>`, `<FutureScalarMetadata>optional</FutureScalarMetadata>`, 1)
	document := syntheticGeminiDocument("user@example.invalid", "conversation", turn)
	document = strings.Replace(document, "</Conversation>", "<Timestamp>2026-01-02T03:04:05Z</Timestamp></Conversation>", 1)
	result, err := collectGeminiExport(t, syntheticGeminiZIP(t, geminiExportFile{Name: "owner.xml", Content: []byte(document)}))
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), result[0].Timestamp)
	// A recognized conversation plus another nonempty unrecognized conversation
	// must not appear to be a successful partial import.
	document = strings.Replace(document, "</Conversations>", `<Conversation><ConversationId>unsupported</ConversationId><UnknownTurn>body</UnknownTurn></Conversation></Conversations>`, 1)
	result, err = collectGeminiExport(t, syntheticGeminiZIP(t, geminiExportFile{Name: "owner.xml", Content: []byte(document)}))
	require.Error(t, err)
	require.Empty(t, result)
}

func TestGeminiExportOrderIsIndependentOfArchiveOrder(t *testing.T) {
	t.Parallel()
	oldDocument := syntheticGeminiDocument("person@example.com", "conversation", syntheticGeminiTurn)
	newDocument := strings.ReplaceAll(oldDocument, "2026-01-02T03:04:05+0000", "2026-01-03T03:04:05+0000")
	oldFile := geminiExportFile{Name: "older.xml", Content: []byte(oldDocument)}
	newFile := geminiExportFile{Name: "newer.xml", Content: []byte(newDocument)}
	forward, err := collectGeminiExport(t, syntheticGeminiZIP(t, oldFile, newFile))
	require.NoError(t, err)
	reversed, err := collectGeminiExport(t, syntheticGeminiZIP(t, newFile, oldFile))
	require.NoError(t, err)
	require.Equal(t, forward, reversed)
	require.Len(t, reversed, 2)
	require.True(t, reversed[0].Timestamp.Before(reversed[1].Timestamp))
}

// The element structure matches a locally validated Google Vault download.
// All identities, text, IDs, model names, and dates below are synthetic.
const syntheticObservedGeminiXML = `<GeminiUserConversationHistory>
<User><Email>person@example.invalid</Email></User>
<Conversations><Conversation>
<ConversationId>synthetic-conversation</ConversationId>
<ConversationTopic>Synthetic release checklist</ConversationTopic>
<ConversationTurns>
<ConversationTurn>
<RequestId>synthetic-request-one</RequestId><ModelVersion>synthetic-model</ModelVersion>
<Timestamp>2026-01-02T03:04:05Z</Timestamp>
<Prompt><Text>Draft a checklist &amp; explain &lt;rollout&gt;.</Text></Prompt>
<PrimaryResponse><ResponseId>synthetic-response-one</ResponseId><Text><![CDATA[Check tests <first>.]]></Text></PrimaryResponse>
</ConversationTurn>
<ConversationTurn>
<RequestId>synthetic-request-two</RequestId><ModelVersion>synthetic-model</ModelVersion>
<Timestamp>2026-01-02T03:05:05.123456Z</Timestamp>
<Prompt><Text>Add a rollback step.</Text></Prompt>
<PrimaryResponse><ResponseId>synthetic-response-two</ResponseId><Text>Restore the prior version.</Text></PrimaryResponse>
<ParentRequestId>synthetic-request-one</ParentRequestId>
</ConversationTurn>
</ConversationTurns>
</Conversation></Conversations>
</GeminiUserConversationHistory>`

func TestGeminiExportObservedVaultXMLLayout(t *testing.T) {
	t.Parallel()
	archive := syntheticGeminiZIP(t, geminiExportFile{Name: "owner.xml", Content: []byte(syntheticObservedGeminiXML)})
	turns, err := collectGeminiExport(t, archive)
	require.NoError(t, err)
	require.Len(t, turns, 2)
	require.Equal(t, "person@example.invalid", turns[0].OwnerEmail)
	require.Equal(t, "synthetic-conversation", turns[0].ConversationID)
	require.Equal(t, "Synthetic release checklist", turns[0].Title)
	require.Equal(t, "synthetic-model", turns[0].Model)
	require.Equal(t, "synthetic-request-one", turns[0].RequestID)
	require.Empty(t, turns[0].ParentRequestID)
	require.Equal(t, "Draft a checklist & explain <rollout>.", turns[0].Prompt)
	require.Equal(t, "Check tests <first>.", turns[0].Response)
	require.Equal(t, "synthetic-request-one", turns[1].ParentRequestID)
	require.Equal(t, time.Date(2026, 1, 2, 3, 5, 5, 123456000, time.UTC), turns[1].Timestamp)
	require.NotEqual(t, turns[0].ID, turns[1].ID)
	replay, err := collectGeminiExport(t, archive)
	require.NoError(t, err)
	require.Equal(t, turns, replay)
	namespaced := strings.Replace(syntheticObservedGeminiXML, "<GeminiUserConversationHistory>", `<GeminiUserConversationHistory xmlns="urn:synthetic-vault">`, 1)
	parsed, err := parseGeminiConversationXML(t.Context(), []byte(namespaced))
	require.NoError(t, err)
	require.Equal(t, turns, parsed)
}

func TestGeminiExportObservedVaultXMLRejectsAmbiguousFields(t *testing.T) {
	t.Parallel()
	for name, document := range map[string]string{
		"missing owner":            strings.Replace(syntheticObservedGeminiXML, "<User><Email>person@example.invalid</Email></User>", "", 1),
		"duplicate owner":          strings.Replace(syntheticObservedGeminiXML, "</User>", "</User><User><Email>other@example.invalid</Email></User>", 1),
		"duplicate email":          strings.Replace(syntheticObservedGeminiXML, "</User>", "<Email>other@example.invalid</Email></User>", 1),
		"duplicate prompt text":    strings.Replace(syntheticObservedGeminiXML, "</Prompt>", "<Text>extra</Text></Prompt>", 1),
		"mixed prompt text":        strings.Replace(syntheticObservedGeminiXML, "<Prompt>", "<Prompt>unrecognized text", 1),
		"unknown nested content":   strings.Replace(syntheticObservedGeminiXML, "</Prompt>", "<Unknown><Text>extra</Text></Unknown></Prompt>", 1),
		"missing response text":    strings.Replace(syntheticObservedGeminiXML, "<Text><![CDATA[Check tests <first>.]]></Text>", "", 1),
		"nested response metadata": strings.Replace(syntheticObservedGeminiXML, "<ResponseId>synthetic-response-one</ResponseId>", "<ResponseId><Nested>value</Nested></ResponseId>", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseGeminiConversationXML(t.Context(), []byte(document))
			require.Error(t, err)
		})
	}
}
