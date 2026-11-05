package remotesessions

import (
	"net/http"
	"sync"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
)

// RefreshAttribution says where a refresh's connection row goes; every refreshing path names one.
type RefreshAttribution struct {
	scope   mcpconnectionattempts.Scope
	attempt mcpconnectionattempts.Attempt
	skipped *SkippedClients
	client  mcpconnectionattempts.Option
}

// RefreshIn files a refresh as its own attempt under scope.
func RefreshIn(scope mcpconnectionattempts.Scope) RefreshAttribution {
	return RefreshAttribution{scope: scope, attempt: mcpconnectionattempts.NoAttempt, skipped: nil, client: mcpconnectionattempts.Option{}}
}

// InAttempt makes the refresh a step of attempt. A zero attempt changes nothing.
func (a RefreshAttribution) InAttempt(attempt mcpconnectionattempts.Attempt) RefreshAttribution {
	if !attempt.IsZero() {
		a.attempt = attempt
	}
	return a
}

// NotingSkips collects why partial resolution left a client out.
func (a RefreshAttribution) NotingSkips(skipped *SkippedClients) RefreshAttribution {
	a.skipped = skipped
	return a
}

// SkippedClients holds why partial resolution left clients out, since a token map entry changes routing.
type SkippedClients struct {
	mu      sync.Mutex
	skipped []skippedClient
}

type skippedClient struct {
	projectID uuid.UUID
	issuerID  uuid.UUID
	clientID  uuid.UUID
	reason    error
}

// Note records why a project's client was left out. Nil ignores it.
func (s *SkippedClients) Note(projectID, issuerID, clientID uuid.UUID, reason error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skipped = append(s.skipped, skippedClient{projectID: projectID, issuerID: issuerID, clientID: clientID, reason: reason})
}

// Reason is why a member's client was left out, or nil. A chosen client must match exactly.
func (s *SkippedClients) Reason(projectID, issuerID uuid.UUID, chosenClient uuid.NullUUID) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, skip := range s.skipped {
		if skip.projectID == projectID && skip.issuerID == issuerID && (!chosenClient.Valid || skip.clientID == chosenClient.UUID) {
			return skip.reason
		}
	}
	return nil
}

// FromRequest copies what the request says about its client now, since a refresh can outlive the request.
func (a RefreshAttribution) FromRequest(r *http.Request) RefreshAttribution {
	a.client = mcpconnectionattempts.FromRequest(r)
	return a
}
