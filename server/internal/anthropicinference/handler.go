package anthropicinference

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Config binds a webhook endpoint to a trusted Gram project and Anthropic tenant.
// Secrets are deployment configuration and must never be committed.
type Config struct {
	// ID selects the URL suffix for this integration.
	ID string `json:"id"`

	// OrganizationID is the trusted Gram organization binding.
	OrganizationID string `json:"organization_id"`

	// ProjectID selects storage and policy scope within that organization.
	ProjectID uuid.UUID `json:"project_id"`

	// TenantID must match the signed Anthropic tenant identifier.
	TenantID string `json:"tenant_id"`

	// SigningSecrets contains the active Standard Webhooks secrets, including rotation overlap.
	SigningSecrets []string `json:"signing_secrets"`
}

// Processor persists the transcript and evaluates the project's security policies.
type Processor interface {
	Process(context.Context, Config, Frame) (Verdict, error)
}

type handler struct {
	config    Config
	keys      [][]byte
	processor Processor
	logger    *slog.Logger
}

var endpointIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Attach validates deployment configuration before registering signed webhooks.
func Attach(mux goahttp.Muxer, logger *slog.Logger, processor Processor, rawConfig string) error {
	if strings.TrimSpace(rawConfig) == "" {
		return nil
	}
	var configs []Config
	if err := json.Unmarshal([]byte(rawConfig), &configs); err != nil {
		// JSON errors can contain portions of secret values.
		return errors.New("invalid Anthropic inference hook configuration JSON")
	}
	handlers := make(map[string]*handler, len(configs))
	for _, config := range configs {
		if !endpointIDPattern.MatchString(config.ID) || config.OrganizationID == "" || config.ProjectID == uuid.Nil || config.TenantID == "" || len(config.SigningSecrets) == 0 {
			return errors.New("anthropic inference hook configuration requires an endpoint id, organization, project, tenant, and signing secrets")
		}
		if _, exists := handlers[config.ID]; exists {
			return errors.New("duplicate Anthropic inference hook endpoint id")
		}
		keys := make([][]byte, 0, len(config.SigningSecrets))
		for _, secret := range config.SigningSecrets {
			key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
			if err != nil || len(key) < 16 {
				return errors.New("invalid Anthropic inference hook signing secret")
			}
			keys = append(keys, key)
		}
		config.SigningSecrets = nil
		handlers[config.ID] = &handler{config: config, keys: keys, processor: processor, logger: logger}
	}
	for id, handler := range handlers {
		mux.Handle(http.MethodPost, "/hooks/anthropic-inference/"+id, handler.ServeHTTP)
	}
	return nil
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid request body", http.StatusBadRequest)
		}
		return
	}
	if err := verifySignature(r.Header, body, h.keys, time.Now()); err != nil {
		http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		return
	}
	var frame Frame
	if err := json.Unmarshal(body, &frame); err != nil {
		http.Error(w, "invalid prompt frame", http.StatusBadRequest)
		return
	}
	if frame.RequestID != r.Header.Get("webhook-id") || (frame.TenantID != "" && frame.TenantID != h.config.TenantID) {
		http.Error(w, "webhook identity mismatch", http.StatusForbidden)
		return
	}
	verdict := Verdict{Action: "allow", DenyReason: "", ReferenceID: ""}
	if frame.Type == "prompt" {
		verdict, err = h.processor.Process(r.Context(), h.config, frame)
		if err != nil {
			h.logger.ErrorContext(r.Context(), "process Anthropic inference hook", attr.SlogError(err))
			// A failed evaluation must not silently allow inference, including when
			// Anthropic's administrator selected allow-on-webhook-failure.
			verdict = Verdict{Action: "deny", DenyReason: "Speakeasy could not evaluate this request. Please try again.", ReferenceID: ""}
		}
	}
	if verdict.Action != "allow" && verdict.Action != "deny" {
		verdict = Verdict{Action: "deny", DenyReason: "Speakeasy could not evaluate this request. Please try again.", ReferenceID: ""}
	}
	verdict.DenyReason = string([]rune(verdict.DenyReason)[:min(len([]rune(verdict.DenyReason)), 500)])
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(verdict); err != nil {
		h.logger.ErrorContext(r.Context(), "write Anthropic inference verdict", attr.SlogError(fmt.Errorf("encode verdict: %w", err)))
	}
}
