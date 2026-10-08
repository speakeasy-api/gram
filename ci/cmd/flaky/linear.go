package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	linearEndpoint = "https://api.linear.app/graphql"

	labelCandidate   = "flaky-candidate"
	labelQuarantined = "flaky-quarantined"

	// titlePrefix starts every flaky-test ticket title; the rest of the title is
	// the testKey. The title is how CI matches a failure to its ticket.
	titlePrefix = "flaky test: "
)

// flakyIssue is an open Linear ticket that tracks one flaky test.
type flakyIssue struct {
	ID          string
	Identifier  string
	URL         string
	Key         testKey
	Quarantined bool
	LabelIDs    []string
}

func issueTitle(k testKey) string {
	return titlePrefix + k.String()
}

func keyFromTitle(title string) (testKey, bool) {
	rest, ok := strings.CutPrefix(title, titlePrefix)
	if !ok {
		return testKey{}, false
	}
	pkg, test, ok := strings.Cut(rest, " ")
	if !ok || pkg == "" || test == "" || strings.Contains(test, " ") {
		return testKey{}, false
	}
	return testKey{Package: pkg, Test: test}, true
}

type linearClient struct {
	apiKey string
	team   string
	http   *http.Client
}

func newLinearClient(apiKey, team string) *linearClient {
	return &linearClient{apiKey: apiKey, team: team, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *linearClient) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return fmt.Errorf("encode linear request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, linearEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build linear request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call linear: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode linear response (status %d): %w", resp.StatusCode, err)
	}
	if len(envelope.Errors) > 0 {
		msgs := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("linear: %s", strings.Join(msgs, "; "))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("linear returned status %d", resp.StatusCode)
	}

	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode linear data: %w", err)
	}
	return nil
}

// openFlakyIssues returns the team's open candidate and quarantined tickets,
// keyed by test.
func (c *linearClient) openFlakyIssues(ctx context.Context) (map[testKey]flakyIssue, error) {
	const query = `query($team: String!, $labels: [String!]!, $after: String) {
  issues(first: 250, after: $after, filter: {
    team: { key: { eq: $team } }
    labels: { some: { name: { in: $labels } } }
    state: { type: { nin: ["completed", "canceled"] } }
  }) {
    nodes { id identifier url title labels { nodes { id name } } }
    pageInfo { hasNextPage endCursor }
  }
}`

	type page struct {
		Issues struct {
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Nodes []struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				URL        string `json:"url"`
				Title      string `json:"title"`
				Labels     struct {
					Nodes []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
			} `json:"nodes"`
		} `json:"issues"`
	}

	issues := map[testKey]flakyIssue{}
	vars := map[string]any{"team": c.team, "labels": []string{labelCandidate, labelQuarantined}, "after": nil}
	for {
		var out page
		if err := c.do(ctx, query, vars, &out); err != nil {
			return nil, fmt.Errorf("list flaky issues: %w", err)
		}

		for _, n := range out.Issues.Nodes {
			key, ok := keyFromTitle(n.Title)
			if !ok {
				continue
			}
			issue := flakyIssue{ID: n.ID, Identifier: n.Identifier, URL: n.URL, Key: key}
			for _, l := range n.Labels.Nodes {
				issue.LabelIDs = append(issue.LabelIDs, l.ID)
				if l.Name == labelQuarantined {
					issue.Quarantined = true
				}
			}
			issues[key] = issue
		}

		if !out.Issues.PageInfo.HasNextPage {
			return issues, nil
		}
		vars["after"] = out.Issues.PageInfo.EndCursor
	}
}

func (c *linearClient) teamID(ctx context.Context) (string, error) {
	var out struct {
		Teams struct {
			Nodes []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	err := c.do(ctx, `query($key: String!) { teams(filter: { key: { eq: $key } }) { nodes { id } } }`,
		map[string]any{"key": c.team}, &out)
	if err != nil {
		return "", fmt.Errorf("find team %s: %w", c.team, err)
	}
	if len(out.Teams.Nodes) != 1 {
		return "", fmt.Errorf("found %d teams with key %s", len(out.Teams.Nodes), c.team)
	}
	return out.Teams.Nodes[0].ID, nil
}

// labelID finds a label by name, creating a workspace label when create is set
// and none exists.
func (c *linearClient) labelID(ctx context.Context, name string, create bool) (string, error) {
	var out struct {
		IssueLabels struct {
			Nodes []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		} `json:"issueLabels"`
	}
	err := c.do(ctx, `query($name: String!) { issueLabels(filter: { name: { eq: $name } }) { nodes { id } } }`,
		map[string]any{"name": name}, &out)
	if err != nil {
		return "", fmt.Errorf("find label %q: %w", name, err)
	}
	if len(out.IssueLabels.Nodes) > 0 {
		return out.IssueLabels.Nodes[0].ID, nil
	}
	if !create {
		return "", fmt.Errorf("label %q does not exist", name)
	}

	var created struct {
		IssueLabelCreate struct {
			IssueLabel struct {
				ID string `json:"id"`
			} `json:"issueLabel"`
		} `json:"issueLabelCreate"`
	}
	err = c.do(ctx, `mutation($name: String!) { issueLabelCreate(input: { name: $name }) { issueLabel { id } } }`,
		map[string]any{"name": name}, &created)
	if err != nil {
		return "", fmt.Errorf("create label %q: %w", name, err)
	}
	return created.IssueLabelCreate.IssueLabel.ID, nil
}

func (c *linearClient) userID(ctx context.Context, name string) (string, error) {
	var out struct {
		Users struct {
			Nodes []struct {
				ID string `json:"id"`
			} `json:"nodes"`
		} `json:"users"`
	}
	err := c.do(ctx, `query($name: String!) { users(filter: { name: { eq: $name } }) { nodes { id } } }`,
		map[string]any{"name": name}, &out)
	if err != nil {
		return "", fmt.Errorf("find user %q: %w", name, err)
	}
	if len(out.Users.Nodes) != 1 {
		return "", fmt.Errorf("found %d users named %q", len(out.Users.Nodes), name)
	}
	return out.Users.Nodes[0].ID, nil
}

func (c *linearClient) createCandidate(ctx context.Context, key testKey, description string) (string, error) {
	teamID, err := c.teamID(ctx)
	if err != nil {
		return "", err
	}
	labelID, err := c.labelID(ctx, labelCandidate, true)
	if err != nil {
		return "", err
	}

	var out struct {
		IssueCreate struct {
			Issue struct {
				Identifier string `json:"identifier"`
			} `json:"issue"`
		} `json:"issueCreate"`
	}
	const mutation = `mutation($input: IssueCreateInput!) { issueCreate(input: $input) { issue { identifier } } }`
	input := map[string]any{
		"teamId":      teamID,
		"title":       issueTitle(key),
		"description": description,
		"labelIds":    []string{labelID},
	}
	if err := c.do(ctx, mutation, map[string]any{"input": input}, &out); err != nil {
		return "", fmt.Errorf("create ticket for %s: %w", key, err)
	}
	return out.IssueCreate.Issue.Identifier, nil
}

// quarantineConfig names the people and labels a quarantined ticket is handed
// to, matching how other autopilot work is routed in Linear.
type quarantineConfig struct {
	Assignee string   // Linear user who owns the ticket
	Delegate string   // Linear agent that works it
	Labels   []string // labels that put the ticket on autopilot
}

// quarantine swaps the candidate label for the quarantined one and hands the
// ticket to the autopilot assignee and delegate.
func (c *linearClient) quarantine(ctx context.Context, issue flakyIssue, cfg quarantineConfig) error {
	candidateID, err := c.labelID(ctx, labelCandidate, true)
	if err != nil {
		return err
	}
	quarantinedID, err := c.labelID(ctx, labelQuarantined, true)
	if err != nil {
		return err
	}

	labelIDs := []string{quarantinedID}
	for _, id := range issue.LabelIDs {
		if id != candidateID && id != quarantinedID {
			labelIDs = append(labelIDs, id)
		}
	}
	for _, name := range cfg.Labels {
		id, err := c.labelID(ctx, name, false)
		if err != nil {
			return err
		}
		labelIDs = append(labelIDs, id)
	}

	input := map[string]any{"labelIds": labelIDs}
	if cfg.Assignee != "" {
		if input["assigneeId"], err = c.userID(ctx, cfg.Assignee); err != nil {
			return err
		}
	}
	if cfg.Delegate != "" {
		if input["delegateId"], err = c.userID(ctx, cfg.Delegate); err != nil {
			return err
		}
	}

	var out struct {
		IssueUpdate struct {
			Success bool `json:"success"`
		} `json:"issueUpdate"`
	}
	const mutation = `mutation($id: String!, $input: IssueUpdateInput!) { issueUpdate(id: $id, input: $input) { success } }`
	if err := c.do(ctx, mutation, map[string]any{"id": issue.ID, "input": input}, &out); err != nil {
		return fmt.Errorf("quarantine %s: %w", issue.Identifier, err)
	}
	if !out.IssueUpdate.Success {
		return errors.New("quarantine " + issue.Identifier + ": update not applied")
	}
	return nil
}

func (c *linearClient) comment(ctx context.Context, issue flakyIssue, body string) error {
	var out struct {
		CommentCreate struct {
			Success bool `json:"success"`
		} `json:"commentCreate"`
	}
	const mutation = `mutation($input: CommentCreateInput!) { commentCreate(input: $input) { success } }`
	input := map[string]any{"issueId": issue.ID, "body": body}
	if err := c.do(ctx, mutation, map[string]any{"input": input}, &out); err != nil {
		return fmt.Errorf("comment on %s: %w", issue.Identifier, err)
	}
	if !out.CommentCreate.Success {
		return errors.New("comment on " + issue.Identifier + ": comment not created")
	}
	return nil
}
