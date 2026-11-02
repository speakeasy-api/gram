package tailscaleapi

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"
)

// Fake is an in-memory Client that records calls for tests and local
// development. It accepts any non-empty credentials unless Reject is set.
type Fake struct {
	mu sync.Mutex

	// Reject, when non-nil, is returned from every call.
	Reject error

	// Now supplies key expiry timestamps.
	Now func() time.Time

	keys     []AuthKey
	services map[string]Service
	deleted  []string
}

var _ Client = (*Fake)(nil)

// NewFake returns an empty fake.
func NewFake() *Fake {
	return &Fake{mu: sync.Mutex{}, Reject: nil, Now: time.Now, keys: nil, services: make(map[string]Service), deleted: nil}
}

func (f *Fake) check(creds Credentials) error {
	if f.Reject != nil {
		return f.Reject
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return ErrUnauthorized
	}
	return nil
}

// CreateAuthKey records a key and returns it with a deterministic secret.
func (f *Fake) CreateAuthKey(_ context.Context, creds Credentials, req AuthKeyRequest) (AuthKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(creds); err != nil {
		return AuthKey{}, err
	}
	if req.Tag == "" || req.TTL <= 0 {
		return AuthKey{}, errors.New("auth key request requires a tag and a ttl")
	}
	key := AuthKey{
		ID:      fmt.Sprintf("k%d", len(f.keys)+1),
		Key:     fmt.Sprintf("tskey-auth-fake-%d-%s", len(f.keys)+1, req.Tag),
		Expires: f.Now().Add(req.TTL),
	}
	f.keys = append(f.keys, key)
	return key, nil
}

// DeleteDevice records the node id.
func (f *Fake) DeleteDevice(_ context.Context, creds Credentials, nodeID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(creds); err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("device deletion requires a node id")
	}
	f.deleted = append(f.deleted, nodeID)
	return nil
}

// UpsertService stores the definition by name.
func (f *Fake) UpsertService(_ context.Context, creds Credentials, svc Service) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(creds); err != nil {
		return err
	}
	if !strings.HasPrefix(svc.Name, "svc:") || len(svc.Ports) == 0 {
		return errors.New("service definition requires a svc: name and at least one port")
	}
	f.services[svc.Name] = svc
	return nil
}

// DeleteService removes the definition; a missing one is success.
func (f *Fake) DeleteService(_ context.Context, creds Credentials, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.check(creds); err != nil {
		return err
	}
	if !strings.HasPrefix(name, "svc:") {
		return errors.New("service deletion requires a svc: name")
	}
	delete(f.services, name)
	return nil
}

// Keys returns every minted key in order.
func (f *Fake) Keys() []AuthKey {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]AuthKey(nil), f.keys...)
}

// DeletedDevices returns every node id passed to DeleteDevice.
func (f *Fake) DeletedDevices() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

// Services returns the current definitions keyed by name.
func (f *Fake) Services() map[string]Service {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]Service, len(f.services))
	maps.Copy(out, f.services)
	return out
}
