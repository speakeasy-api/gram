package email

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/resend"
)

// NewResendService uses published, stable aliases. Loops remains the audience
// store; contact upserts are best effort and never turn a delivered email into
// a failed send (which could cause callers to send it again).
func NewResendService(logger *slog.Logger, sender resend.Client, contacts loops.WorkflowClient, from, replyTo string, enabled bool) *Service {
	logger = logger.With(attr.SlogComponent("email"))
	return &Service{logger: logger, enabled: enabled, send: func(ctx context.Context, recipient, idempotencyKey string, template Template) error {
		selected, err := resendTemplate(template)
		if err != nil {
			return err
		}
		if err := sender.Send(ctx, resend.SendInput{From: from, To: []string{recipient}, ReplyTo: replyTo, Template: selected, IdempotencyKey: idempotencyKey}); err != nil {
			return err
		}
		if template.AddToAudience() && contacts != nil {
			// The shared HTTP client has no overall request timeout. Bound this
			// best-effort follow-up after delivery has already succeeded.
			contactCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if err := contacts.UpdateContact(contactCtx, loops.UpdateContactInput{Email: recipient}); err != nil {
				// Provider errors can contain recipient data; log the operation, not the body.
				logger.WarnContext(ctx, "email delivered but Loops contact upsert failed")
			}
		}
		return nil
	}}
}

func resendTemplate(template Template) (resend.Template, error) {
	key := template.Key()
	registered := false
	for _, candidate := range RegisteredTemplates {
		if candidate.Key() == key {
			registered = true
			break
		}
	}
	if !registered {
		return resend.Template{}, fmt.Errorf("%w: %q", ErrUnregisteredTemplate, key)
	}
	alias := "gram.v2." + string(key)
	variables := maps.Clone(template.Variables())
	switch key {
	case TemplateKeyCustomDomainUnhealthy:
		variables["recipient_email"] = variables["email"]
		delete(variables, "email")
	case TemplateKeyWeeklyUsageSummary:
		if variables["show_estimated_spend"] == "true" {
			alias += "_with_spend"
		} else {
			for _, key := range []string{"storage_cost_usd", "scanning_cost_usd", "egress_cost_usd", "total_cost_usd"} {
				delete(variables, key)
			}
		}
		delete(variables, "show_estimated_spend")
	case TemplateKeyDirectoryReviewPending:
		if variables["is_reminder"] == "true" {
			alias += "_reminder"
		}
		delete(variables, "is_reminder")
	case TemplateKeyOpenRouterChatCredits:
		if variables["exhausted"] == "true" {
			alias = "gram.v2.openrouter_chat_credits_exhausted"
		}
		delete(variables, "exhausted")
	case TemplateKeyOpenRouterInternalCredits:
		if variables["exhausted"] == "true" {
			alias = "gram.v2.openrouter_internal_credits_exhausted"
		}
		delete(variables, "exhausted")
	}
	return resend.Template{ID: alias, Variables: variables}, nil
}
