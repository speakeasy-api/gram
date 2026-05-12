package svix

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/must"
	"github.com/svix/svix-webhooks/go/models"
)

type stub struct {
	logger *slog.Logger
	mux    *http.ServeMux
	apps   sync.Map
}

func NewStubServer(logger *slog.Logger) *httptest.Server {
	var s stub

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/app", s.handleAppCreate)
	mux.HandleFunc("POST /api/v1/app/{appID}/msg", s.handleMessageCreate)
	mux.HandleFunc("POST /api/v1/auth/app-portal-access/{app_id}", s.handleAppPortalAccessCreate)

	s.logger = logger
	s.mux = mux

	return httptest.NewServer(&s)
}

func (s *stub) handleAppCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var inp models.ApplicationIn
	if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	getIfExists, _ := strconv.ParseBool(r.URL.Query().Get("get_if_exists"))
	var out *models.ApplicationOut
	var found bool
	if inp.Uid == nil {
		id := must.Value(uuid.NewV7()).String()
		out = &models.ApplicationOut{
			Id:           id,
			Uid:          inp.Uid,
			Name:         inp.Name,
			RateLimit:    inp.RateLimit,
			ThrottleRate: inp.ThrottleRate,
			Metadata:     *conv.Default(inp.Metadata, new(map[string]string{})),
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
	} else {
		if val, exists := s.apps.Load(*inp.Uid); exists {
			found = true
			if getIfExists {
				out, _ = val.(*models.ApplicationOut)
			} else {
				http.Error(w, "application with this UID already exists", http.StatusConflict)
				return
			}
		} else {
			id := must.Value(uuid.NewV7()).String()
			out = &models.ApplicationOut{
				Id:           id,
				Uid:          inp.Uid,
				Name:         inp.Name,
				RateLimit:    inp.RateLimit,
				ThrottleRate: inp.ThrottleRate,
				Metadata:     *conv.Default(inp.Metadata, new(map[string]string{})),
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}
			s.apps.Store(*inp.Uid, out)
		}
	}

	if err := inv.Check(
		"well formed application result",
		"result is not nil", out != nil && out.Id != "",
	); err != nil {
		http.Error(w, fmt.Sprintf("invalid application result: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(conv.Ternary(found, http.StatusOK, http.StatusCreated))
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.logger.ErrorContext(ctx, "failed to write stub svix response", attr.SlogError(err))
		return
	}
}

func (s *stub) handleMessageCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var inp models.MessageIn
	if err := json.NewDecoder(r.Body).Decode(&inp); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	id := must.Value(uuid.NewV7()).String()
	out := models.MessageOut{
		Id:        id,
		Channels:  inp.Channels,
		DeliverAt: inp.DeliverAt,
		EventId:   inp.EventId,
		EventType: inp.EventType,
		Payload:   inp.Payload,
		Tags:      inp.Tags,
		Timestamp: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.logger.ErrorContext(ctx, "failed to write stub svix response", attr.SlogError(err))
		return
	}
}

func (s *stub) handleAppPortalAccessCreate(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "svix app portal access unavailable in local development", http.StatusNotImplemented)
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
