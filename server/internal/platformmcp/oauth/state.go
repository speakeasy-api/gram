// Package oauth defines Platform MCP's organization-bound OAuth state contracts.
// It intentionally has no HTTP, hosted MCP, or database dependency.
package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound       = errors.New("platform oauth state not found")
	ErrRevoked        = errors.New("platform oauth state revoked")
	ErrExpired        = errors.New("platform oauth state expired")
	ErrAlreadyUsed    = errors.New("platform oauth state already used")
	ErrClientMismatch = errors.New("platform oauth client mismatch")
	ErrGeneration     = errors.New("platform oauth connection generation mismatch")
	ErrRedirectURI    = errors.New("platform oauth redirect URI mismatch")
	ErrPKCE           = errors.New("platform oauth PKCE mismatch")
)

type Client struct {
	ID              string
	SecretHash      string
	Name            string
	RedirectURIs    []string
	SecretExpiresAt *time.Time
	RevokedAt       *time.Time
}

type Connection struct {
	ID             string
	ClientID       string
	Subject        string
	OrganizationID string
	Generation     string
	RevokedAt      *time.Time
}

type Grant struct {
	Code          string
	ClientID      string
	Connection    Connection
	RedirectURI   string
	CodeChallenge string
	ExpiresAt     time.Time
}

type ConsumeGrantInput struct {
	OrganizationID string
	Code           string
	ClientID       string
	RedirectURI    string
	CodeVerifier   string
	Now            time.Time
}

type Session struct {
	ID               string
	ClientID         string
	Connection       Connection
	JTI              string
	RefreshHash      string
	ExpiresAt        time.Time
	RefreshExpiresAt time.Time
	RevokedAt        *time.Time
}

type RotateSessionInput struct {
	OrganizationID string
	RefreshHash    string
	ClientID       string
	Generation     string
	Now            time.Time
	Replacement    Session
}

type AuthorizeConnectionInput struct {
	Connection Connection
	Grant      Grant
	Now        time.Time
}

// Store defines the state transitions the Platform MCP authorization server requires.
type Store interface {
	RegisterClient(ctx context.Context, client Client) error
	GetClient(ctx context.Context, clientID string) (Client, error)
	RevokeClient(ctx context.Context, clientID string, now time.Time) error
	RegisterConnection(ctx context.Context, connection Connection) error
	GetConnection(ctx context.Context, organizationID, subject, clientID string) (Connection, error)
	AuthorizeConnection(ctx context.Context, input AuthorizeConnectionInput) (Connection, error)
	RevokeConnection(ctx context.Context, organizationID, connectionID string, now time.Time) error
	IssueGrant(ctx context.Context, grant Grant) error
	ConsumeGrant(ctx context.Context, input ConsumeGrantInput) (Grant, error)
	CreateSession(ctx context.Context, session Session) error
	GetSessionByRefreshHash(ctx context.Context, organizationID, refreshHash string) (Session, error)
	DetectRefreshReuse(ctx context.Context, organizationID, refreshHash string, now time.Time) (bool, error)
	RotateSession(ctx context.Context, input RotateSessionInput) (Session, error)
	RevokeSession(ctx context.Context, organizationID, refreshHash, clientID string, now time.Time) (Session, error)
	RevokeAccessSession(ctx context.Context, organizationID, jti, clientID string, now time.Time) (Session, error)
	RotateConnectionGeneration(ctx context.Context, organizationID, connectionID, generation string, now time.Time) (Connection, error)
}

// InMemoryStore is a concurrency-safe contract implementation for Platform OAuth.
type InMemoryStore struct {
	mu          sync.Mutex
	clients     map[string]Client
	grants      map[string]Grant
	connections map[string]Connection
	sessions    map[string]Session
}

var _ Store = (*InMemoryStore)(nil)

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		mu:          sync.Mutex{},
		clients:     map[string]Client{},
		grants:      map[string]Grant{},
		connections: map[string]Connection{},
		sessions:    map[string]Session{},
	}
}

func (s *InMemoryStore) RegisterClient(_ context.Context, client Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.clients[client.ID]; exists {
		return ErrAlreadyUsed
	}
	s.clients[client.ID] = client
	return nil
}

func (s *InMemoryStore) GetClient(_ context.Context, clientID string) (Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, ok := s.clients[clientID]
	if !ok {
		return Client{}, ErrNotFound
	}
	if client.RevokedAt != nil {
		return Client{}, ErrRevoked
	}
	return client, nil
}

func (s *InMemoryStore) RevokeClient(_ context.Context, clientID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	client, ok := s.clients[clientID]
	if !ok {
		return ErrNotFound
	}
	client.RevokedAt = &now
	s.clients[clientID] = client
	return nil
}

func (s *InMemoryStore) RegisterConnection(_ context.Context, connection Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateClient(connection.ClientID); err != nil {
		return err
	}
	if _, exists := s.connections[connection.ID]; exists {
		return ErrAlreadyUsed
	}
	if connection.RevokedAt == nil {
		for _, existing := range s.connections {
			if existing.OrganizationID == connection.OrganizationID && existing.Subject == connection.Subject && existing.ClientID == connection.ClientID && existing.RevokedAt == nil {
				return ErrAlreadyUsed
			}
		}
	}
	s.connections[connection.ID] = connection
	return nil
}

func (s *InMemoryStore) GetConnection(_ context.Context, organizationID, subject, clientID string) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateClient(clientID); err != nil {
		return Connection{}, err
	}
	for _, connection := range s.connections {
		if connection.OrganizationID == organizationID && connection.Subject == subject && connection.ClientID == clientID && connection.RevokedAt == nil {
			return connection, nil
		}
	}
	return Connection{}, ErrNotFound
}

func (s *InMemoryStore) AuthorizeConnection(_ context.Context, input AuthorizeConnectionInput) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateClient(input.Connection.ClientID); err != nil {
		return Connection{}, err
	}
	if input.Grant.ClientID != input.Connection.ClientID {
		return Connection{}, ErrClientMismatch
	}
	if _, exists := s.grants[input.Grant.Code]; exists {
		return Connection{}, ErrAlreadyUsed
	}
	connection := input.Connection
	for id, existing := range s.connections {
		if existing.OrganizationID != connection.OrganizationID || existing.Subject != connection.Subject || existing.ClientID != connection.ClientID || existing.RevokedAt != nil {
			continue
		}
		s.revokeGeneration(id, existing.Generation, input.Now)
		existing.Generation = connection.Generation
		s.connections[id] = existing
		connection = existing
		break
	}
	if _, exists := s.connections[connection.ID]; !exists {
		s.connections[connection.ID] = connection
	}
	grant := input.Grant
	grant.Connection = connection
	s.grants[grant.Code] = grant
	return connection, nil
}

func (s *InMemoryStore) RevokeConnection(_ context.Context, organizationID, connectionID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	connection, ok := s.connections[connectionID]
	if !ok || connection.OrganizationID != organizationID {
		return ErrNotFound
	}
	connection.RevokedAt = &now
	s.connections[connectionID] = connection
	s.revokeGeneration(connectionID, connection.Generation, now)
	return nil
}

func (s *InMemoryStore) IssueGrant(_ context.Context, grant Grant) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateConnection(grant.Connection, grant.ClientID); err != nil {
		return err
	}
	if !validPKCES256Challenge(grant.CodeChallenge) {
		return ErrPKCE
	}
	if _, exists := s.grants[grant.Code]; exists {
		return ErrAlreadyUsed
	}
	s.grants[grant.Code] = grant
	return nil
}

func (s *InMemoryStore) ConsumeGrant(_ context.Context, input ConsumeGrantInput) (Grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	grant, ok := s.grants[input.Code]
	if !ok {
		return Grant{}, ErrAlreadyUsed
	}
	if grant.ClientID != input.ClientID {
		return Grant{}, ErrClientMismatch
	}
	if grant.Connection.OrganizationID != input.OrganizationID {
		return Grant{}, ErrNotFound
	}
	if !input.Now.Before(grant.ExpiresAt) {
		delete(s.grants, input.Code)
		return Grant{}, ErrExpired
	}
	if grant.RedirectURI != input.RedirectURI {
		return Grant{}, ErrRedirectURI
	}
	if !verifyPKCES256(input.CodeVerifier, grant.CodeChallenge) {
		return Grant{}, ErrPKCE
	}
	if err := s.validateConnection(grant.Connection, grant.ClientID); err != nil {
		return Grant{}, err
	}
	delete(s.grants, input.Code)
	return grant, nil
}

func (s *InMemoryStore) CreateSession(_ context.Context, session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.validateConnection(session.Connection, session.ClientID); err != nil {
		return err
	}
	if _, exists := s.sessions[session.RefreshHash]; exists {
		return ErrAlreadyUsed
	}
	for _, existing := range s.sessions {
		if existing.JTI == session.JTI {
			return ErrAlreadyUsed
		}
	}
	s.sessions[session.RefreshHash] = session
	return nil
}

func (s *InMemoryStore) GetSessionByRefreshHash(_ context.Context, organizationID, refreshHash string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[refreshHash]
	if !ok || session.Connection.OrganizationID != organizationID {
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (s *InMemoryStore) DetectRefreshReuse(_ context.Context, organizationID, refreshHash string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[refreshHash]
	if !ok || session.Connection.OrganizationID != organizationID {
		return false, ErrNotFound
	}
	if session.RevokedAt == nil {
		return false, nil
	}
	s.revokeGeneration(session.Connection.ID, session.Connection.Generation, now)
	return true, nil
}

func (s *InMemoryStore) RotateSession(_ context.Context, input RotateSessionInput) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[input.RefreshHash]
	if !ok {
		return Session{}, ErrAlreadyUsed
	}
	if session.Connection.OrganizationID != input.OrganizationID || input.Replacement.Connection.OrganizationID != input.OrganizationID {
		return Session{}, ErrNotFound
	}
	if session.ClientID != input.ClientID || input.Replacement.ClientID != input.ClientID {
		return Session{}, ErrClientMismatch
	}
	if session.Connection.ID != input.Replacement.Connection.ID || session.Connection.Generation != input.Generation || input.Replacement.Connection.Generation != input.Generation {
		return Session{}, ErrGeneration
	}
	if _, err := s.validateConnectionRevocation(session.Connection, input.ClientID); err != nil {
		return Session{}, err
	}
	if session.RevokedAt != nil {
		s.revokeGeneration(session.Connection.ID, session.Connection.Generation, input.Now)
		return Session{}, ErrAlreadyUsed
	}
	if !input.Now.Before(session.RefreshExpiresAt) {
		session.RevokedAt = &input.Now
		s.sessions[input.RefreshHash] = session
		return Session{}, ErrExpired
	}
	if input.Replacement.RefreshHash == input.RefreshHash {
		return Session{}, ErrAlreadyUsed
	}
	if err := s.validateConnection(input.Replacement.Connection, input.ClientID); err != nil {
		return Session{}, err
	}
	if _, exists := s.sessions[input.Replacement.RefreshHash]; exists {
		return Session{}, ErrAlreadyUsed
	}

	session.RevokedAt = &input.Now
	s.sessions[input.RefreshHash] = session
	s.sessions[input.Replacement.RefreshHash] = input.Replacement
	return session, nil
}

func (s *InMemoryStore) RevokeSession(_ context.Context, organizationID, refreshHash, clientID string, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[refreshHash]
	if !ok || session.Connection.OrganizationID != organizationID || session.ClientID != clientID {
		return Session{}, ErrNotFound
	}
	session.RevokedAt = &now
	s.sessions[refreshHash] = session
	return session, nil
}

func (s *InMemoryStore) RevokeAccessSession(_ context.Context, organizationID, jti, clientID string, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var revoked Session
	var found bool
	for hash, session := range s.sessions {
		if session.Connection.OrganizationID == organizationID && session.JTI == jti && session.ClientID == clientID && session.RevokedAt == nil {
			session.RevokedAt = &now
			s.sessions[hash] = session
			revoked = session
			found = true
		}
	}
	if !found {
		return Session{}, ErrNotFound
	}
	return revoked, nil
}

func (s *InMemoryStore) RotateConnectionGeneration(_ context.Context, organizationID, connectionID, generation string, now time.Time) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	connection, ok := s.connections[connectionID]
	if !ok || connection.OrganizationID != organizationID {
		return Connection{}, ErrNotFound
	}
	if connection.RevokedAt != nil {
		return Connection{}, ErrRevoked
	}
	s.revokeGeneration(connectionID, connection.Generation, now)
	connection.Generation = generation
	s.connections[connectionID] = connection
	return connection, nil
}

func (s *InMemoryStore) revokeGeneration(connectionID, generation string, now time.Time) {
	for hash, session := range s.sessions {
		if session.Connection.ID == connectionID && session.Connection.Generation == generation {
			session.RevokedAt = &now
			s.sessions[hash] = session
		}
	}
}

func (s *InMemoryStore) validateClient(clientID string) error {
	client, ok := s.clients[clientID]
	if !ok {
		return ErrNotFound
	}
	if client.RevokedAt != nil {
		return ErrRevoked
	}
	return nil
}

func (s *InMemoryStore) validateConnection(connection Connection, clientID string) error {
	stored, err := s.validateConnectionRevocation(connection, clientID)
	if err != nil {
		return err
	}
	if connection.ClientID != clientID || stored.ClientID != clientID || stored.Subject != connection.Subject || stored.OrganizationID != connection.OrganizationID {
		return ErrClientMismatch
	}
	if stored.Generation != connection.Generation {
		return ErrGeneration
	}
	return nil
}

func (s *InMemoryStore) validateConnectionRevocation(connection Connection, clientID string) (Connection, error) {
	if err := s.validateClient(clientID); err != nil {
		return Connection{}, err
	}
	stored, ok := s.connections[connection.ID]
	if !ok {
		return Connection{}, ErrNotFound
	}
	if stored.RevokedAt != nil {
		return Connection{}, ErrRevoked
	}
	return stored, nil
}

func verifyPKCES256(verifier, challenge string) bool {
	if !validPKCEVerifier(verifier) || !validPKCES256Challenge(challenge) {
		return false
	}
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:]) == challenge
}

func validPKCEVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	return strings.IndexFunc(verifier, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && !strings.ContainsRune("-._~", r)
	}) == -1
}

func validPKCES256Challenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	return strings.IndexFunc(challenge, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '_'
	}) == -1
}
