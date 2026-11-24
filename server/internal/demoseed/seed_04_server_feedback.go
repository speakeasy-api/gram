package demoseed

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/demoseed/repo"
)

// serverFeedbackConfig is one server's feedback tool settings. An empty
// toolName leaves the tool under its default name.
type serverFeedbackConfig struct {
	server         uuid.UUID
	toolName       string
	changedBy      string
	changedDaysAgo int32
}

// serverFeedbackReport is one report an agent sent about the OpenAPI Server.
// Empty strings are fields the agent left out; user 0 is a caller who was not
// signed in, which a public server allows.
type serverFeedbackReport struct {
	minutesAgo int32
	severity   string
	tool       string
	content    string
	intention  string
	workaround string
	model      string
	user       int
	externalID string
	client     string
	clientVer  string
}

const (
	feedbackMinutesPerHour = 60
	feedbackMinutesPerDay  = 24 * feedbackMinutesPerHour
)

// serverFeedbackReports spread over three weeks so the 30-day counts, the
// ranking of tools and the list all have something to show: every severity,
// praise with none, a report about the server as a whole, callers who were
// members, signed in through the customer's identity provider or anonymous,
// one long report, one with a secret already masked, and one older than the
// window the counts cover.
var serverFeedbackReports = []serverFeedbackReport{
	{2 * feedbackMinutesPerHour, "blocked", "process_refund",
		"process_refund rejects every amount with \"amount must be a positive integer\", including 4999. The description says the field is a number in dollars, but the error reads as if it wants cents. I could not issue the refund either way.",
		"Refund an order in full after a duplicate charge.",
		"None. I told the user to issue it from the billing console.",
		"claude-sonnet-4-6 in Claude Code", 5, "", "claude-code", "2.1.34"},
	{5 * feedbackMinutesPerHour, "degraded", "search_logs",
		"search_logs returns at most 100 lines and gives no cursor or next-page token, so a busy hour cannot be read in one go.",
		"Find every 502 from the checkout service between 14:00 and 15:00.",
		"Called it twelve times with five-minute windows.",
		"gpt-5.6 in Cursor", 4, "", "cursor", "1.7.52"},
	{1 * feedbackMinutesPerDay, "blocked", "query_db",
		"query_db times out after 30 seconds on any query that joins orders to refunds. There is no way to ask for longer or to page the result.",
		"Count refunds per plan for last month.",
		"",
		"claude-sonnet-4-6 in Claude Code", 3, "", "claude-code", "2.1.34"},
	{1*feedbackMinutesPerDay + 4*feedbackMinutesPerHour, "cosmetic", "get_customer",
		"get_customer takes an email or an ID in the same \"customer\" field, and the description does not say which wins when a value could be either. I guessed ID.",
		"", "",
		"claude-sonnet-5 in Claude", 2, "", "claude-ai", ""},
	{2 * feedbackMinutesPerDay, "degraded", "search_logs",
		"The \"level\" filter is case-sensitive: \"error\" returns nothing and \"ERROR\" works. The description lists the values in lower case.",
		"Pull the errors around a failed deploy.",
		"Retried with the value in upper case.",
		"claude-sonnet-4-6 in Claude Code", 1, "", "claude-code", "2.1.31"},
	{2*feedbackMinutesPerDay + 6*feedbackMinutesPerHour, "", "fetch_traces",
		"fetch_traces is a pleasure to use: the span tree comes back already ordered, and the summary field saved a second call.",
		"", "",
		"gpt-5.6 in Codex", 6, "", "codex", "0.42.0"},
	{3 * feedbackMinutesPerDay, "blocked", "process_refund",
		"process_refund answered 403 \"refunds above 500 require approval\", and no tool on this server requests that approval.",
		"Refund an annual plan charged twice.",
		"Asked the user to get the approval and come back.",
		"claude-sonnet-4-6 in Claude Code", 5, "", "claude-code", "2.1.34"},
	{4 * feedbackMinutesPerDay, "degraded", "get_metrics",
		"get_metrics needs the exact metric name and nothing lists the names. It took six guesses to land on \"http.server.duration\".",
		"Chart request latency for the API over the last day.",
		"Guessed names from OpenTelemetry conventions until one returned data.",
		"gpt-5.6 in Cursor", 4, "", "cursor", "1.7.52"},
	{5 * feedbackMinutesPerDay, "cosmetic", "list_deploys",
		"list_deploys returns timestamps with no timezone. I assumed UTC.",
		"", "",
		"claude-sonnet-4-6 in Claude Code", 3, "", "claude-code", "2.1.31"},
	{6 * feedbackMinutesPerDay, "blocked", "query_db",
		"The call failed and put a connection string in the error: \"could not connect: postgres://reporting:[REDACTED]@db.internal:5432/reporting\". I could not run the query, and the error says more than it should.",
		"List the ten largest refunds this quarter.",
		"",
		"gpt-5.6 in Cursor", 4, "", "cursor", "1.7.48"},
	{7 * feedbackMinutesPerDay, "degraded", "search_logs",
		"search_logs cannot leave a service out, so health-check noise fills the 100 lines it returns.",
		"Read the application errors from the last ten minutes.",
		"Added a text filter for \"ERROR\" and accepted that some lines were missed.",
		"claude-sonnet-5 in Claude", 2, "", "claude-ai", ""},
	{8 * feedbackMinutesPerDay, "cosmetic", "check_health",
		"check_health reports \"degraded\" without saying which dependency is unhealthy.",
		"", "",
		"gpt-5.6 in Codex", 0, "", "codex", "0.42.0"},
	{9 * feedbackMinutesPerDay, "degraded", "process_refund",
		"A partial refund needs line item IDs, which come from an order lookup this server does not offer.",
		"Refund one item from a three-item order.",
		"Asked the user to paste the line item IDs.",
		"claude-sonnet-4-6 in Claude Code", 1, "", "claude-code", "2.1.29"},
	{10 * feedbackMinutesPerDay, "blocked", "search_logs",
		"Every call failed with 429 for about ten minutes, with no Retry-After and nothing in the message about when to try again.",
		"Investigate a spike in checkout failures.",
		"Waited and retried by hand.",
		"claude-sonnet-5 in Claude", 0, "okta|00uexamplecontractor7", "claude-ai", ""},
	{12 * feedbackMinutesPerDay, "cosmetic", "get_customer",
		"The \"plan\" field comes back as an internal code such as \"p_ent_v3\". A readable name would save a lookup.",
		"", "",
		"claude-sonnet-4-6 in Claude Code", 5, "", "claude-code", "2.1.29"},
	{13 * feedbackMinutesPerDay, "", "",
		"The server as a whole is easy to work with: tool names say what they do and the errors are specific. The one gap is refunds, where I keep needing a person.",
		"", "",
		"gpt-5.6 in Codex", 6, "", "codex", "0.41.2"},
	{15 * feedbackMinutesPerDay, "degraded", "fetch_traces",
		"fetch_traces accepts a trace ID only. Starting from a request ID in the logs, I had to search the logs a second time for the trace ID.",
		"Follow one failed request from the edge to the database.",
		"Searched the logs for the request ID and read the trace ID out of a line.",
		"gpt-5.6 in Cursor", 4, "", "cursor", "1.7.44"},
	{17 * feedbackMinutesPerDay, "blocked", "process_refund",
		"process_refund fails for any order paid in a currency other than USD. The request is accepted, the call takes about twenty seconds, and then it returns 500 with the body \"internal error\" and nothing else. I tried the same order three times, then an order in EUR and one in GBP from other customers, and each failed the same way, while two USD orders went through on the first try. The description does not mention currency at all, so I cannot tell whether this is a limit of the tool or a fault behind it. It would help to either say that only USD is supported and name the place to send the others, or to return an error that says which part failed, because as it stands the user waited a minute for an answer that told them nothing and I had no way to finish what they asked for.",
		"Refund three orders for a customer in Berlin who was charged twice during an outage.",
		"None for the EUR and GBP orders. I listed them for the user to refund by hand.",
		"claude-sonnet-4-6 in Claude Code", 1, "", "claude-code", "2.1.24"},
	{20 * feedbackMinutesPerDay, "cosmetic", "query_db",
		"Column names in the result are upper case, while the schema the server describes has them in lower case.",
		"", "",
		"claude-sonnet-4-6 in Claude Code", 3, "", "claude-code", "2.1.24"},
	{22 * feedbackMinutesPerDay, "degraded", "get_metrics",
		"The description gives the \"step\" parameter in milliseconds, but it is read as seconds: a step of 60000 returned one point every sixteen hours.",
		"Plot error rate per minute during an incident.",
		"Divided the step by a thousand.",
		"claude-sonnet-5 in Claude", 2, "", "claude-ai", ""},
	{38 * feedbackMinutesPerDay, "cosmetic", "search_logs",
		"The time range is called \"from\" and \"to\" here and \"start\" and \"end\" on get_metrics.",
		"", "",
		"gpt-5.6 in Cursor", 4, "", "cursor", "1.7.31"},
}

// seedServerFeedback switches the feedback tool on for the OpenAPI Server and
// files reports against it. It is the demo's one server built from an API,
// the only kind that offers the feedback tool; the others belong to third
// parties, so their pages have no Agent Feedback tab.
func (s *seeder) seedServerFeedback(ctx context.Context) error {
	org := s.orgID()
	proj := s.projectID()

	configs := []serverFeedbackConfig{
		{server: s.supportServer(), toolName: "", changedBy: "priya", changedDaysAgo: 40},
	}
	for _, config := range configs {
		if err := s.q.ServerFeedbackInsertConfig(ctx, repo.ServerFeedbackInsertConfigParams{
			McpServerID:     config.server,
			OrgID:           org,
			ProjectID:       proj,
			Enabled:         true,
			ToolName:        conv.ToPGTextEmpty(config.toolName),
			UpdatedByUserID: s.userID(config.changedBy),
			ChangedDaysAgo:  config.changedDaysAgo,
		}); err != nil {
			return fmt.Errorf("insert demo mcp server feedback config: %w", err)
		}
	}

	for i, report := range serverFeedbackReports {
		userID := ""
		if report.user > 0 {
			userID = s.userID(demoUsers[report.user-1].handle)
		}
		if err := s.q.ServerFeedbackInsertReport(ctx, repo.ServerFeedbackInsertReportParams{
			ID:             s.det(fmt.Sprintf("mcpfeedback-%d", i+1)),
			OrgID:          org,
			ProjectID:      proj,
			McpServerID:    s.supportServer(),
			Content:        report.content,
			Severity:       conv.ToPGTextEmpty(report.severity),
			Workaround:     conv.ToPGTextEmpty(report.workaround),
			Intention:      conv.ToPGTextEmpty(report.intention),
			ToolName:       conv.ToPGTextEmpty(report.tool),
			Model:          conv.ToPGTextEmpty(report.model),
			UserID:         conv.ToPGTextEmpty(userID),
			ExternalUserID: conv.ToPGTextEmpty(report.externalID),
			ClientName:     conv.ToPGTextEmpty(report.client),
			ClientVersion:  conv.ToPGTextEmpty(report.clientVer),
			MinutesAgo:     report.minutesAgo,
		}); err != nil {
			return fmt.Errorf("insert demo mcp server feedback %d: %w", i+1, err)
		}
	}
	return nil
}
