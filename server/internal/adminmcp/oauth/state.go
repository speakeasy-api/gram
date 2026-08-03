// Package oauth defines Admin MCP's organization-bound OAuth state contracts.
// It intentionally has no HTTP, hosted MCP, or database dependency.
package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	ErrNotFound       = errors.New("admin oauth state not found")
	ErrRevoked        = errors.New("admin oauth state revoked")
	ErrExpired        = errors.New("admin oauth state expired")
	ErrAlreadyUsed    = errors.New("admin oauth state already used")
	ErrClientMismatch = errors.New("admin oauth client mismatch")
	ErrGeneration     = errors.New("admin oauth connection generation mismatch")
	ErrRedirectURI    = errors.New("admin oauth redirect URI mismatch")
	ErrPKCE           = errors.New("admin oauth PKCE mismatch")
)

type Client struct {
	ID        string
	RevokedAt *time.Time
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
	Code         string
	ClientID     string
	RedirectURI  string
	CodeVerifier string
	Now          time.Time
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
	RefreshHash string
	ClientID    string
	Generation  string
	Now         time.Time
	Replacement Session
}

// Store defines the state transitions the Admin authorization server requires.
type Store interface {
	RegisterClient(ctx context.Context, client Client) error
	RevokeClient(ctx context.Context, clientID string, now time.Time) error
	RegisterConnection(ctx context.Context, connection Connection) error
	RevokeConnection(ctx context.Context, connectionID string, now time.Time) error
	IssueGrant(ctx context.Context, grant Grant) error
	ConsumeGrant(ctx context.Context, input ConsumeGrantInput) (Grant, error)
	CreateSession(ctx context.Context, session Session) error
	RotateSession(ctx context.Context, input RotateSessionInput) (Session, error)
	RevokeSession(ctx context.Context, refreshHash, clientID string, now time.Time) (Session, error)
	RotateConnectionGeneration(ctx context.Context, connectionID, generation string, now time.Time) (Connection, error)
}

// InMemoryStore is a concurrency-safe contract implementation for Admin OAuth.
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
	s.connections[connection.ID] = connection
	return nil
}

func (s *InMemoryStore) RevokeConnection(_ context.Context, connectionID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	connection, ok := s.connections[connectionID]
	if !ok {
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
	if input.Now.After(grant.ExpiresAt) {
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
	s.sessions[session.RefreshHash] = session
	return nil
}

func (s *InMemoryStore) RotateSession(_ context.Context, input RotateSessionInput) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[input.RefreshHash]
	if !ok {
		return Session{}, ErrAlreadyUsed
	}
	if session.ClientID != input.ClientID || input.Replacement.ClientID != input.ClientID {
		return Session{}, ErrClientMismatch
	}
	if session.Connection.ID != input.Replacement.Connection.ID || session.Connection.Generation != input.Generation || input.Replacement.Connection.Generation != input.Generation {
		return Session{}, ErrGeneration
	}
	if session.RevokedAt != nil {
		s.revokeGeneration(session.Connection.ID, session.Connection.Generation, input.Now)
		return Session{}, ErrAlreadyUsed
	}
	if input.Now.After(session.RefreshExpiresAt) {
		session.RevokedAt = &input.Now
		s.sessions[input.RefreshHash] = session
		return Session{}, ErrExpired
	}
	if input.Replacement.RefreshHash == input.RefreshHash {
		return Session{}, ErrAlreadyUsed
	}
	if err := s.validateConnection(session.Connection, input.ClientID); err != nil {
		return Session{}, err
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

func (s *InMemoryStore) RevokeSession(_ context.Context, refreshHash, clientID string, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[refreshHash]
	if !ok || session.ClientID != clientID {
		return Session{}, ErrNotFound
	}
	session.RevokedAt = &now
	s.sessions[refreshHash] = session
	return session, nil
}

func (s *InMemoryStore) RotateConnectionGeneration(_ context.Context, connectionID, generation string, now time.Time) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	connection, ok := s.connections[connectionID]
	if !ok {
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
	if err := s.validateClient(clientID); err != nil {
		return err
	}
	stored, ok := s.connections[connection.ID]
	if !ok {
		return ErrNotFound
	}
	if stored.RevokedAt != nil {
		return ErrRevoked
	}
	if stored.ClientID != clientID || stored.Subject != connection.Subject || stored.OrganizationID != connection.OrganizationID {
		return ErrClientMismatch
	}
	if stored.Generation != connection.Generation {
		return ErrGeneration
	}
	return nil
}

func verifyPKCES256(verifier, challenge string) bool {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:]) == challenge
}
