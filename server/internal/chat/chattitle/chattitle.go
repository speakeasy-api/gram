// Package chattitle holds the chat title rules that packages below chat in
// the import graph (telemetry, hooks) apply as well, so they cannot live in
// package chat itself.
package chattitle

import (
	"regexp"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// MaxRunes bounds a title derived from message content. Titles render in
// dense lists, so a long first prompt is cut rather than allowed to dominate
// the row.
const MaxRunes = 80

// cronJobPrompt matches the envelope OpenClaw opens a cron run's first prompt
// with: "[cron:<job uuid> <job name>] ...". The job name is the only part a
// reader recognizes.
var cronJobPrompt = regexp.MustCompile(`^\s*\[cron:[0-9a-fA-F-]{36} ([^\]]+)\]`)

// CronJobTitle names a chat opened by an OpenClaw cron run after its job,
// "Cron: <job name>", instead of the envelope its first prompt starts with.
// It reports false when content is not a cron run's prompt.
func CronJobTitle(content string) (string, bool) {
	match := cronJobPrompt.FindStringSubmatch(content)
	if match == nil {
		return "", false
	}
	name := strings.TrimSpace(match[1])
	if name == "" {
		return "", false
	}
	return conv.TruncateString("Cron: "+name, MaxRunes), true
}

// Display is the title to show for a stored chat title. Cron chats captured
// before CronJobTitle existed stored their raw envelope; those are shown
// under their job name. A title someone set by hand is shown as is.
func Display(title string, manuallySet bool) string {
	if manuallySet {
		return title
	}
	if cronTitle, ok := CronJobTitle(title); ok {
		return cronTitle
	}
	return title
}
