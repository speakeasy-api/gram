package slackapp

import (
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// HomeData is a permission-filtered snapshot. Enabled means both permitted and
// available for the organization; an error must never read as an empty queue.
type HomeData struct {
	DashboardURL, OrganizationName, Notice                    string
	Linked                                                    bool
	ApprovalsEnabled, MCPRequestsEnabled, RiskEnabled         bool
	ApprovalsError, MCPRequestsError, RiskError, ServersError bool
	Approvals                                                 []HomeApproval
	MCPRequests                                               []HomeMCPRequest
	Risks                                                     []HomeRisk
	Servers                                                   []HomeServer
}

// HomeApproval is an open tool call the viewer is allowed to review.
type HomeApproval struct {
	ID, ProjectID, PolicyName, ToolName, RequesterName, URL string
	CanDecide                                               bool
}

// HomeMCPRequest links to a server review in the dashboard.
type HomeMCPRequest struct{ Name, ProjectName, URL string }

// HomeRisk deliberately excludes matched content, arguments and evidence.
type HomeRisk struct {
	PolicyName, Severity, ProjectName, URL string
	CreatedAt                              time.Time
}

// HomeServer links to an MCP the viewer has permission to see. LogoURL points
// to its existing public branding asset, not an arbitrary vendor image URL.
type HomeServer struct{ Name, ProjectName, URL, LogoURL string }

// HomeView renders compact Block Kit cards. Titles use safe, bounded mrkdwn;
// supporting text is kept to one line, with no untrusted Slack control syntax.
func HomeView(data HomeData) map[string]any {
	blocks := []any{homeHeader("Your Speakeasy workspace")}
	if !data.Linked {
		blocks = append(blocks, homeSection("Connect your workspace", "Ask your organization admin to link your Slack account in Speakeasy.", "", ""))
		if data.Notice != "" {
			blocks = append(blocks, homeContext(data.Notice))
		}
		blocks = append(blocks, homeDashboard(data.DashboardURL))
		return map[string]any{"type": "home", "blocks": blocks}
	}
	if data.OrganizationName != "" {
		blocks = append(blocks, homeContext(data.OrganizationName))
	}
	if data.Notice != "" {
		blocks = append(blocks, homeContext(data.Notice))
	}

	anyQueue := data.ApprovalsEnabled || data.MCPRequestsEnabled || data.RiskEnabled
	failed := data.ApprovalsError || data.MCPRequestsError || data.RiskError
	if anyQueue && !failed && len(data.Approvals) == 0 && len(data.MCPRequests) == 0 && len(data.Risks) == 0 {
		blocks = append(blocks, homeDivider(), homeSection("✅ All clear", "Nothing needs your attention.", "", ""))
	}
	if data.ApprovalsError {
		blocks = append(blocks, homeUnavailable("Tool approvals"))
	} else if data.ApprovalsEnabled && len(data.Approvals) > 0 {
		blocks = append(blocks, homeDivider(), homeHeader("Waiting on your review"))
		for i, item := range data.Approvals {
			if i == 5 {
				break
			}
			if i > 0 {
				blocks = append(blocks, homeDivider())
			}
			detail := homeCompact(item.RequesterName, 70) + " · " + homeCompact(item.PolicyName, 90)
			if !item.CanDecide {
				detail = "Waiting for another approver · " + homeCompact(item.PolicyName, 90)
			}
			blocks = append(blocks, homeSection(item.ToolName, detail, "", ""))
			buttons := []any{}
			if item.CanDecide {
				value := item.ProjectID + ":" + item.ID
				approve := homeButton("Approve once", homeClickApprove, "")
				approve["style"], approve["value"] = "primary", value
				approve["confirm"] = homeConfirm("Approve this call?", "Allow the tool call under its requested scope. Review the details first.", "Approve once", "primary")
				deny := homeButton("Deny", homeClickDeny, "")
				deny["style"], deny["value"] = "danger", value
				deny["confirm"] = homeConfirm("Deny this call?", "The requested tool call will not proceed.", "Deny", "danger")
				buttons = append(buttons, approve, deny)
			}
			if item.URL != "" {
				buttons = append(buttons, homeButton("Details ↗", "home_open_dashboard", item.URL))
			}
			if len(buttons) > 0 {
				blocks = append(blocks, map[string]any{"type": "actions", "elements": buttons})
			}
		}
	}
	if data.MCPRequestsError {
		blocks = append(blocks, homeUnavailable("MCP requests"))
	} else if data.MCPRequestsEnabled && len(data.MCPRequests) > 0 {
		blocks = append(blocks, homeDivider(), homeHeader("MCP requests"))
		for i, item := range data.MCPRequests {
			if i == 5 {
				break
			}
			blocks = append(blocks, homeSection(item.Name, item.ProjectName, "Review ↗", item.URL))
		}
	}
	if data.RiskError {
		blocks = append(blocks, homeUnavailable("Risk events"))
	} else if data.RiskEnabled && len(data.Risks) > 0 {
		blocks = append(blocks, homeDivider(), homeHeader("Recent risk events"))
		for i, item := range data.Risks {
			if i == 5 {
				break
			}
			severity, indicator := "High", "🟠"
			if item.Severity == "critical" {
				severity, indicator = "Critical", "🔴"
			}
			detail := severity + " · " + homeCompact(item.ProjectName, 70)
			if !item.CreatedAt.IsZero() {
				detail += " · " + item.CreatedAt.UTC().Format("2 Jan 2006, 15:04 UTC")
			}
			blocks = append(blocks, homeSection(indicator+" "+item.PolicyName, detail, "View ↗", item.URL))
		}
	}

	blocks = append(blocks, homeDivider(), homeHeader("Your MCPs"))
	if data.ServersError {
		blocks = append(blocks, homeUnavailable("MCPs"))
	} else if len(data.Servers) == 0 {
		blocks = append(blocks, homeSection("No MCPs yet", "Ask your organization admin for access.", "", ""))
	} else {
		for i, item := range data.Servers {
			if i == 5 {
				break
			}
			if i > 0 {
				blocks = append(blocks, homeDivider())
			}
			card := homeSection(item.Name, item.ProjectName, "", "")
			if homeImageURL(item.LogoURL) {
				card["accessory"] = map[string]any{"type": "image", "image_url": item.LogoURL, "alt_text": homeCompact(item.Name+" logo", 100)}
			}
			blocks = append(blocks, card)
			if item.URL != "" {
				blocks = append(blocks, map[string]any{"type": "actions", "elements": []any{homeButton("Open MCP ↗", "home_open_dashboard", item.URL)}})
			}
		}
	}
	blocks = append(blocks, homeDivider(), homeDashboard(data.DashboardURL))
	return map[string]any{"type": "home", "blocks": blocks}
}

func homeText(text string) map[string]any {
	return map[string]any{"type": "plain_text", "text": homeFit(text, 2900), "emoji": true}
}
func homeHeader(text string) map[string]any {
	return map[string]any{"type": "header", "text": homeText(text)}
}
func homeDivider() map[string]any { return map[string]any{"type": "divider"} }
func homeContext(text string) map[string]any {
	return map[string]any{"type": "context", "elements": []any{homeText(homeCompact(text, 220))}}
}
func homeSection(title, detail, label, link string) map[string]any {
	text := homeLabel(title, true)
	if detail != "" {
		text += "\n" + homeLabel(detail, false)
	}
	block := map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": text, "verbatim": true}}
	if link != "" {
		block["accessory"] = homeButton(label, "home_open_dashboard", link)
	}
	return block
}

var homeEmojiCode = regexp.MustCompile(`:[a-zA-Z0-9_+\-]+:`)

// homeLabel keeps labels on one line and escapes Slack links/mentions. Labels
// containing formatting markers render as inline code, so they cannot break out
// of the title or impersonate the card's own formatting.
func homeLabel(text string, bold bool) string {
	text = homeCompact(text, 180)
	safe := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
	if strings.ContainsAny(text, "*_~`") || homeEmojiCode.MatchString(text) {
		return "`" + strings.ReplaceAll(safe, "`", "'") + "`"
	}
	if bold {
		return "*" + safe + "*"
	}
	return safe
}
func homeCompact(text string, limit int) string {
	return homeFit(strings.Join(strings.Fields(text), " "), limit)
}
func homeImageURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	// Slack fetches images itself. Local-only origins would make publishing
	// fail with an image download error rather than just hide the logo.
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "localhost" || !strings.Contains(host, ".") {
		return false
	}
	for _, suffix := range []string{".local", ".localhost", ".internal", ".test", ".invalid"} {
		if strings.HasSuffix(host, suffix) {
			return false
		}
	}
	if address, err := netip.ParseAddr(host); err == nil {
		return address.IsGlobalUnicast() && !address.IsPrivate()
	}
	return true
}
func homeButton(label, actionID, link string) map[string]any {
	button := map[string]any{"type": "button", "text": homeText(label), "action_id": actionID}
	if link != "" {
		button["url"] = link
	}
	return button
}
func homeConfirm(title, message, confirm, style string) map[string]any {
	return map[string]any{"title": homeText(title), "text": homeText(message), "confirm": homeText(confirm), "deny": homeText("Cancel"), "style": style}
}
func homeDashboard(link string) map[string]any {
	if link == "" {
		return homeContext("Open Speakeasy to manage your workspace.")
	}
	return map[string]any{"type": "actions", "elements": []any{homeButton("Open dashboard ↗", "home_open_dashboard", link)}}
}
func homeUnavailable(section string) map[string]any {
	return homeSection(fmt.Sprintf("%s temporarily unavailable", section), "Try the dashboard.", "", "")
}
func homeFit(text string, limit int) string {
	text = strings.ToValidUTF8(text, "�")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
}
