// Package chattitle holds the chat title rules that packages below chat in
// the import graph (telemetry, hooks) apply as well, so they cannot live in
// package chat itself.
package chattitle

import (
	"regexp"
	"strings"
	"unicode/utf8"

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

// truncatedCronEnvelope matches a stored derived title cut at MaxRunes inside
// the envelope's job name, before its closing bracket.
var truncatedCronEnvelope = regexp.MustCompile(`^\s*\[cron:[0-9a-fA-F-]{36} ([^\]]+)$`)

// StoredCronTitle names a chat after its cron job from a stored title that is
// a cron run's raw envelope, as cron chats captured before CronJobTitle
// existed stored it. That title was cut to MaxRunes, which can end inside a
// long job name; the name is then shown as far as it was kept. It reports
// false when title is not such an envelope.
func StoredCronTitle(title string) (string, bool) {
	if cronTitle, ok := CronJobTitle(title); ok {
		return cronTitle, true
	}
	if utf8.RuneCountInString(title) != MaxRunes {
		return "", false
	}
	match := truncatedCronEnvelope.FindStringSubmatch(title)
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
	if cronTitle, ok := StoredCronTitle(title); ok {
		return cronTitle
	}
	return title
}
