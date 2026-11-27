package triggers

import "context"

// SlackBotTokenForTest exposes slackBotToken to black-box package tests in triggers_test.
func (a *App) SlackBotTokenForTest(ctx context.Context, triggerInstanceID string) (string, error) {
	return a.slackBotToken(ctx, triggerInstanceID)
}
