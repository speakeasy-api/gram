package anthropicinference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// Config is the trusted organization binding resolved from encrypted integration storage.
type Config struct {
	// ID identifies the integration's stable webhook URL.
	ID string

	// OrganizationID owns the integration.
	OrganizationID string

	// ProjectID scopes transcript storage and security policies.
	ProjectID uuid.UUID

	// TenantID optionally restricts a signed provider tenant identifier.
	TenantID string

	// SigningSecrets contains the current secret and unexpired rotation overlap.
	SigningSecrets []string
}

// ConfigResolver resolves a random endpoint identifier to its trusted binding.
// The identifier is a lookup key, not an authentication credential.
type ConfigResolver interface {
	Resolve(context.Context, string) (Config, error)
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

// Attach mounts the webhook once. Configuration changes take effect on the next request.
func Attach(mux goahttp.Muxer, logger *slog.Logger, processor Processor, resolver ConfigResolver) {
	mux.Handle(http.MethodPost, "/hooks/anthropic-inference/{id}", func(w http.ResponseWriter, r *http.Request) {
		config, err := resolver.Resolve(r.Context(), mux.Vars(r)["id"])
		if err != nil {
			http.Error(w, "integration unavailable", http.StatusNotFound)
			return
		}
		keys := make([][]byte, 0, len(config.SigningSecrets))
		for _, secret := range config.SigningSecrets {
			key, err := DecodeSigningSecret(secret)
			if err != nil {
				http.Error(w, "integration unavailable", http.StatusServiceUnavailable)
				return
			}
			keys = append(keys, key)
		}
		config.SigningSecrets = nil
		h := &handler{config: config, keys: keys, processor: processor, logger: logger}
		h.ServeHTTP(w, r)
	})
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
	// Before the first secret is saved, only Anthropic's synthetic setup probe
	// is acknowledged. It never reaches transcript storage or policy evaluation.
	if len(h.keys) == 0 {
		var probe Frame
		if json.Unmarshal(body, &probe) == nil && probe.Type == "prompt" && probe.Source.Application == "config-test" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"action":"allow"}`))
			return
		}
		http.Error(w, "signing secret is not configured", http.StatusUnauthorized)
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
	if frame.RequestID != r.Header.Get("webhook-id") || (h.config.TenantID != "" && frame.TenantID != "" && frame.TenantID != h.config.TenantID) {
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
