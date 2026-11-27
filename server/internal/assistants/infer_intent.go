package assistants

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

//go:embed infer_intent_prompt.txt
var inferIntentSystemPrompt string

const (
	// inferIntentModel is a small, fast model: the answer has to arrive while
	// the user is still looking at the assistant they just opened.
	inferIntentModel   = "anthropic/claude-haiku-4.5"
	inferIntentTimeout = 15 * time.Second

	// The dashboard asks once per assistant open, so a user well inside these
	// limits is opening and closing the assistant by hand.
	inferIntentRatePerMin = 10
	inferIntentRateBurst  = 5

	inferIntentMaxTokens  = 800
	maxSuggestedPrompts   = 3
	maxSuggestedPromptLen = 120
	maxIntentFieldLen     = 500
)

// IntentCompleter is the structured-output completion call InferIntent makes.
type IntentCompleter interface {
	GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error)
}

// SetIntentCompleter enables InferIntent. Without a completer, InferIntent
// returns an empty intent and the dashboard falls back to its default prompts.
func (s *Service) SetIntentCompleter(completer IntentCompleter, store ratelimit.Store) {
	s.intentCompleter = completer
	s.intentLimiter = ratelimit.New(store, "assistant-infer-intent",
		ratelimit.PerMinute(inferIntentRatePerMin).WithBurst(inferIntentRateBurst))
}

func emptyIntent() *gen.AssistantIntent {
	return &gen.AssistantIntent{
		Goal:             nil,
		Stage:            nil,
		Blocker:          nil,
		Frustration:      "none",
		Opener:           nil,
		SuggestedPrompts: []string{},
		Confidence:       0,
	}
}

func (s *Service) InferIntent(ctx context.Context, payload *gen.InferIntentPayload) (*gen.AssistantIntent, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	assistantID, err := parseAssistantID(payload.AssistantID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assistant id").LogError(ctx, s.logger)
	}
	if err := s.authz.Require(ctx, authz.Assistant(authz.AssistantIDOf(assistantID), authz.ProjectIDOf(*authCtx.ProjectID)).Read()); err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "inferring intent requires a user identity").LogError(ctx, s.logger)
	}

	prompt := buildIntentPrompt(conv.PtrValOr(payload.CurrentPath, ""), payload.Activity)
	if s.intentCompleter == nil || prompt == "" {
		return emptyIntent(), nil
	}

	if s.intentLimiter != nil {
		switch res, err := s.intentLimiter.Allow(ctx, authCtx.UserID); {
		case err != nil:
			s.logger.WarnContext(ctx, "infer intent rate limiter unavailable, allowing", attr.SlogError(err))
		case !res.Allowed:
			return nil, oops.E(oops.CodeRateLimitExceeded, nil, "intent inference rate limit exceeded")
		}
	}

	inferCtx, cancel := context.WithTimeout(ctx, inferIntentTimeout)
	defer cancel()

	strict := true
	response, err := s.intentCompleter.GetObjectCompletion(inferCtx, openrouter.ObjectCompletionRequest{
		OrgID:          authCtx.ActiveOrganizationID,
		ProjectID:      authCtx.ProjectID.String(),
		Model:          inferIntentModel,
		SystemPrompt:   inferIntentSystemPrompt,
		Prompt:         prompt,
		Temperature:    nil,
		UsageSource:    billing.ModelUsageSourceGram,
		UserID:         "",
		ExternalUserID: "",
		UserEmail:      "",
		HTTPMetadata:   nil,
		JSONSchema: &or.ChatJSONSchemaConfig{
			Name:        "assistant_intent",
			Schema:      intentSchema(),
			Description: nil,
			Strict:      optionalnullable.From(&strict),
		},
		MaxTokens:              new(inferIntentMaxTokens),
		KeyType:                openrouter.KeyTypeInternal,
		KeySlot:                "",
		Reasoning:              nil,
		DisableResponseHealing: false,
	})
	if err != nil {
		// The opener is a convenience. A failed inference leaves the
		// assistant on its default prompts rather than failing the open.
		s.logger.WarnContext(ctx, "infer assistant intent", attr.SlogError(err), attr.SlogAssistantID(assistantID.String()))
		return emptyIntent(), nil
	}
	if response == nil || response.Message == nil {
		return emptyIntent(), nil
	}

	intent, err := parseIntent(openrouter.GetText(*response.Message))
	if err != nil {
		s.logger.WarnContext(ctx, "decode assistant intent", attr.SlogError(err), attr.SlogAssistantID(assistantID.String()))
		return emptyIntent(), nil
	}
	return intent, nil
}

// buildIntentPrompt renders the activity journal for the model. It returns ""
// when there is no activity, so cold opens skip the model call.
func buildIntentPrompt(currentPath string, activity []*gen.AssistantActivityEntry) string {
	lines := make([]string, 0, len(activity))
	for _, entry := range activity {
		if entry == nil {
			continue
		}
		detail := cleanIntentText(entry.Detail)
		if detail == "" {
			continue
		}
		at := entry.At
		if t, err := time.Parse(time.RFC3339, entry.At); err == nil {
			at = t.UTC().Format("15:04:05")
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s", at, entry.Kind, detail))
	}
	if len(lines) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("<current_page>")
	b.WriteString(cleanIntentText(currentPath))
	b.WriteString("</current_page>\n<activity>\n")
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteString("\n</activity>")
	return b.String()
}

// cleanIntentText folds a journal value onto one line and removes angle
// brackets, so an entry cannot close the tags that mark it as data.
func cleanIntentText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '<' || r == '>':
			return -1
		case unicode.IsControl(r):
			return ' '
		default:
			return r
		}
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

type intentResponse struct {
	Goal             string   `json:"goal"`
	Stage            string   `json:"stage"`
	Blocker          string   `json:"blocker"`
	Frustration      string   `json:"frustration"`
	Opener           string   `json:"opener"`
	SuggestedPrompts []string `json:"suggested_prompts"`
	Confidence       float64  `json:"confidence"`
}

func intentSchema() map[string]any {
	text := map[string]any{"type": "string"}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"goal":              text,
			"stage":             text,
			"blocker":           text,
			"frustration":       map[string]any{"type": "string", "enum": []string{"none", "low", "medium", "high"}},
			"opener":            text,
			"suggested_prompts": map[string]any{"type": "array", "items": text},
			"confidence":        map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		},
		"required":             []string{"goal", "stage", "blocker", "frustration", "opener", "suggested_prompts", "confidence"},
		"additionalProperties": false,
	}
}

func parseIntent(text string) (*gen.AssistantIntent, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("empty intent response")
	}
	var parsed intentResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal intent response: %w", err)
	}

	intent := emptyIntent()
	intent.Goal = conv.PtrEmpty(clip(parsed.Goal, maxIntentFieldLen))
	intent.Stage = conv.PtrEmpty(clip(parsed.Stage, maxIntentFieldLen))
	intent.Blocker = conv.PtrEmpty(clip(parsed.Blocker, maxIntentFieldLen))
	intent.Opener = conv.PtrEmpty(clip(parsed.Opener, maxIntentFieldLen))
	switch parsed.Frustration {
	case "low", "medium", "high":
		intent.Frustration = parsed.Frustration
	}
	intent.Confidence = min(max(parsed.Confidence, 0), 1)
	for _, p := range parsed.SuggestedPrompts {
		p = clip(p, maxSuggestedPromptLen)
		if p == "" {
			continue
		}
		intent.SuggestedPrompts = append(intent.SuggestedPrompts, p)
		if len(intent.SuggestedPrompts) == maxSuggestedPrompts {
			break
		}
	}
	return intent, nil
}

func clip(s string, maxRunes int) string {
	return conv.TruncateString(strings.TrimSpace(s), maxRunes)
}
